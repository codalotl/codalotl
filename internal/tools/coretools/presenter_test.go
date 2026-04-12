package coretools

import (
	"encoding/json"
	"testing"

	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolsWithoutPresentersReturnNil(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)

	tools := []llmstream.Tool{
		NewEditTool(auth),
		NewWriteTool(auth),
	}

	for _, tool := range tools {
		assert.Nil(t, tool.Presenter())
	}
}

func TestApplyPatchPresenter(t *testing.T) {
	patch := `*** Begin Patch
*** Add File: foo/new.txt
+first line
+second line
*** Delete File: foo/old.txt
*** Update File: foo/bar.go
*** Move to: foo/baz.go
@@
 context line
-old line
+new line
@@
+final line
*** End Patch
`

	tests := []struct {
		name  string
		tool  llmstream.Tool
		input string
	}{
		{
			name:  "freeform",
			tool:  NewApplyPatchTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), true, nil),
			input: patch,
		},
		{
			name: "function",
			tool: func() llmstream.Tool {
				return NewApplyPatchTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), false, nil)
			}(),
			input: func() string {
				payload, err := json.Marshal(map[string]any{"patch": patch})
				require.NoError(t, err)
				return string(payload)
			}(),
		},
	}

	expectedDiff := llmstream.Diff{
		Edits: []llmstream.DiffEdit{
			{
				Kind:    llmstream.DiffEditKindAdd,
				NewPath: "foo/new.txt",
				Lines: []llmstream.DiffLine{
					{Kind: llmstream.DiffLineKindAdd, Text: "first line"},
					{Kind: llmstream.DiffLineKindAdd, Text: "second line"},
				},
			},
			{
				Kind:    llmstream.DiffEditKindDelete,
				OldPath: "foo/old.txt",
			},
			{
				Kind:    llmstream.DiffEditKindRename,
				OldPath: "foo/bar.go",
				NewPath: "foo/baz.go",
				Lines: []llmstream.DiffLine{
					{Kind: llmstream.DiffLineKindContext, Text: "context line"},
					{Kind: llmstream.DiffLineKindDelete, Text: "old line"},
					{Kind: llmstream.DiffLineKindAdd, Text: "new line"},
					{Kind: llmstream.DiffLineKindOmitted},
					{Kind: llmstream.DiffLineKindAdd, Text: "final line"},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			presenter := tc.tool.Presenter()
			require.NotNil(t, presenter)

			call := llmstream.ToolCall{
				Name:  ToolNameApplyPatch,
				Input: tc.input,
			}
			result := &llmstream.ToolResult{Name: ToolNameApplyPatch, Result: `{"success":true}`}

			callPresentation := presenter.Present(call, nil)
			resultPresentation := presenter.Present(call, result)

			assert.Equal(t, llmstream.CompletionBehaviorReplace, callPresentation.Behavior)
			assert.Equal(t, callPresentation, resultPresentation)
			assert.False(t, callPresentation.Summary.JoinWithSpace)
			require.Len(t, callPresentation.Summary.Segments, 1)
			assert.Equal(t, llmstream.RoleAction, callPresentation.Summary.Segments[0].Role)
			assert.Equal(t, "Apply Patch", callPresentation.Summary.Segments[0].Text)
			require.Len(t, callPresentation.Body, 1)

			diff, ok := callPresentation.Body[0].(llmstream.Diff)
			require.True(t, ok)
			assert.Equal(t, expectedDiff, diff)
		})
	}
}

func TestApplyPatchPresenter_InvalidPatchHasSummaryOnly(t *testing.T) {
	tool := NewApplyPatchTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), true, nil)
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	presentation := presenter.Present(llmstream.ToolCall{
		Name:  ToolNameApplyPatch,
		Input: "not a patch",
	}, nil)

	assert.Equal(t, llmstream.CompletionBehaviorReplace, presentation.Behavior)
	require.Len(t, presentation.Summary.Segments, 1)
	assert.Equal(t, "Apply Patch", presentation.Summary.Segments[0].Text)
	assert.Empty(t, presentation.Body)
}

func TestDeletePresenter(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewDeleteTool(auth)
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  ToolNameDelete,
		Input: `{"path":"some/file.txt"}`,
	}
	result := &llmstream.ToolResult{Name: ToolNameDelete, Result: "Deleted file: some/file.txt"}

	callPresentation := presenter.Present(call, nil)
	resultPresentation := presenter.Present(call, result)

	assert.Equal(t, llmstream.CompletionBehaviorReplace, callPresentation.Behavior)
	assert.Equal(t, callPresentation, resultPresentation)
	assert.True(t, callPresentation.Summary.JoinWithSpace)
	require.Len(t, callPresentation.Summary.Segments, 2)
	assert.Equal(t, llmstream.RoleAction, callPresentation.Summary.Segments[0].Role)
	assert.Equal(t, "Delete", callPresentation.Summary.Segments[0].Text)
	assert.Equal(t, llmstream.RoleNormal, callPresentation.Summary.Segments[1].Role)
	assert.Equal(t, "some/file.txt", callPresentation.Summary.Segments[1].Text)
	assert.Empty(t, callPresentation.Body)
}

func TestDeletePresenter_FallsBackToToolName(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewDeleteTool(auth)
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	presentation := presenter.Present(llmstream.ToolCall{Name: ToolNameDelete, Input: `{"path":"   "}`}, nil)
	assert.True(t, presentation.Summary.JoinWithSpace)
	require.Len(t, presentation.Summary.Segments, 2)
	assert.Equal(t, "delete", presentation.Summary.Segments[1].Text)

	presentation = presenter.Present(llmstream.ToolCall{Input: `{"path":`}, nil)
	assert.True(t, presentation.Summary.JoinWithSpace)
	require.Len(t, presentation.Summary.Segments, 2)
	assert.Equal(t, "delete", presentation.Summary.Segments[1].Text)
}

func TestLsPresenter(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewLsTool(auth)
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  ToolNameLS,
		Input: `{"path":"some/dir"}`,
	}
	result := &llmstream.ToolResult{Name: ToolNameLS, Result: "<ls>ignored</ls>"}

	callPresentation := presenter.Present(call, nil)
	resultPresentation := presenter.Present(call, result)

	assert.Equal(t, llmstream.CompletionBehaviorReplace, callPresentation.Behavior)
	assert.Equal(t, callPresentation, resultPresentation)
	assert.True(t, callPresentation.Summary.JoinWithSpace)
	require.Len(t, callPresentation.Summary.Segments, 2)
	assert.Equal(t, llmstream.RoleAction, callPresentation.Summary.Segments[0].Role)
	assert.Equal(t, "List", callPresentation.Summary.Segments[0].Text)
	assert.Equal(t, llmstream.RoleNormal, callPresentation.Summary.Segments[1].Role)
	assert.Equal(t, "some/dir", callPresentation.Summary.Segments[1].Text)
	assert.Empty(t, callPresentation.Body)
}

func TestLsPresenter_FallsBackToToolName(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewLsTool(auth)
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	presentation := presenter.Present(llmstream.ToolCall{Name: ToolNameLS, Input: `{"path":"   "}`}, nil)
	assert.True(t, presentation.Summary.JoinWithSpace)
	require.Len(t, presentation.Summary.Segments, 2)
	assert.Equal(t, "ls", presentation.Summary.Segments[1].Text)
}

func TestReadFilePresenter(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewReadFileTool(auth)
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  ToolNameReadFile,
		Input: `{"path":"some/file.txt","line_numbers":true}`,
	}
	result := &llmstream.ToolResult{Name: ToolNameReadFile, Result: "<file>ignored</file>"}

	callPresentation := presenter.Present(call, nil)
	resultPresentation := presenter.Present(call, result)

	assert.Equal(t, llmstream.CompletionBehaviorReplace, callPresentation.Behavior)
	assert.Equal(t, callPresentation, resultPresentation)
	assert.True(t, callPresentation.Summary.JoinWithSpace)
	require.Len(t, callPresentation.Summary.Segments, 2)
	assert.Equal(t, llmstream.RoleAction, callPresentation.Summary.Segments[0].Role)
	assert.Equal(t, "Read", callPresentation.Summary.Segments[0].Text)
	assert.Equal(t, llmstream.RoleNormal, callPresentation.Summary.Segments[1].Role)
	assert.Equal(t, "some/file.txt", callPresentation.Summary.Segments[1].Text)
	assert.Empty(t, callPresentation.Body)
}

func TestReadFilePresenter_FallsBackToToolName(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewReadFileTool(auth)
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	presentation := presenter.Present(llmstream.ToolCall{Name: ToolNameReadFile, Input: `{"path":"   "}`}, nil)
	assert.True(t, presentation.Summary.JoinWithSpace)
	require.Len(t, presentation.Summary.Segments, 2)
	assert.Equal(t, "read_file", presentation.Summary.Segments[1].Text)
}
