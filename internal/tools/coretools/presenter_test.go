package coretools

import (
	"encoding/json"
	"testing"

	"github.com/codalotl/codalotl/internal/applypatch"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEditAndWriteToolsExposePresenters(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)

	tools := []llmstream.Tool{
		NewEditTool(auth),
		NewWriteTool(auth),
	}

	for _, tool := range tools {
		assert.NotNil(t, tool.Presenter())
	}
}

func TestPresenters_SubagentEventPolicyDefault(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)

	tests := []struct {
		name      string
		presenter llmstream.Presenter
	}{
		{name: "apply_patch", presenter: NewApplyPatchTool(auth, true, nil).Presenter()},
		{name: "delete", presenter: NewDeleteTool(auth).Presenter()},
		{name: "edit", presenter: NewEditTool(auth).Presenter()},
		{name: "write", presenter: NewWriteTool(auth).Presenter()},
		{name: "ls", presenter: NewLsTool(auth).Presenter()},
		{name: "read_file", presenter: NewReadFileTool(auth).Presenter()},
		{name: "shell", presenter: NewShellTool(auth).Presenter()},
		{name: "skill_shell", presenter: NewSkillShellTool(auth).Presenter()},
		{name: "update_plan", presenter: NewUpdatePlanTool(auth).Presenter()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.NotNil(t, tc.presenter)
			assert.Equal(t, llmstream.SubagentEventPolicyDefault, tc.presenter.SubagentEventPolicy(llmstream.ToolCall{Name: tc.name}))
		})
	}
}

func TestEditPresenter(t *testing.T) {
	tool := NewEditTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  ToolNameEdit,
		Input: `{"file_path":"foo/bar.go","old_string":"old line","new_string":"new line","replace_all":true}`,
	}
	result := &llmstream.ToolResult{Name: ToolNameEdit, Result: "Edited file: foo/bar.go"}

	callPresentation := presenter.Present(call, nil)
	resultPresentation := presenter.Present(call, result)

	assert.Equal(t, llmstream.CompletionBehaviorReplace, callPresentation.Behavior)
	assert.Equal(t, llmstream.ErrorBehaviorDefault, callPresentation.ErrorBehavior)
	assert.Equal(t, callPresentation, resultPresentation)
	assert.Nil(t, callPresentation.Summary.Segments)

	diff, ok := callPresentation.Body.(llmstream.Diff)
	require.True(t, ok)
	assert.Equal(t, llmstream.Diff{
		Edits: []llmstream.DiffEdit{{
			Kind:       llmstream.DiffEditKindEdit,
			OldPath:    "foo/bar.go",
			ReplaceAll: true,
			Lines: []llmstream.DiffLine{
				{Kind: llmstream.DiffLineKindDelete, Text: "old line"},
				{Kind: llmstream.DiffLineKindAdd, Text: "new line"},
			},
		}},
	}, diff)
}

func TestEditPresenter_ErrorOwnsErrorLine(t *testing.T) {
	tool := NewEditTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	presentation := presenter.Present(llmstream.ToolCall{
		Name:  ToolNameEdit,
		Input: `{"path":"foo/bar.go","old_text":"old line","new_text":"new line"}`,
	}, &llmstream.ToolResult{
		Name:    ToolNameEdit,
		Result:  "replace failed",
		IsError: true,
	})

	assert.Equal(t, llmstream.CompletionBehaviorReplace, presentation.Behavior)
	assert.Equal(t, llmstream.ErrorBehaviorPresenterOwned, presentation.ErrorBehavior)
	assert.Nil(t, presentation.Summary.Segments)

	diff, ok := presentation.Body.(llmstream.Diff)
	require.True(t, ok)
	require.Len(t, diff.Edits, 1)
	require.NotNil(t, diff.Edits[0].Error)
	assert.Equal(t, llmstream.Line{
		Segments: []llmstream.Segment{
			{Text: "Error: replace failed", Role: llmstream.RoleError},
		},
	}, *diff.Edits[0].Error)
}

func TestWritePresenter(t *testing.T) {
	tool := NewWriteTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  ToolNameWrite,
		Input: `{"path":"foo/new.txt","content":"first line\nsecond line"}`,
	}
	result := &llmstream.ToolResult{Name: ToolNameWrite, Result: "Wrote file: foo/new.txt"}

	callPresentation := presenter.Present(call, nil)
	resultPresentation := presenter.Present(call, result)

	assert.Equal(t, llmstream.CompletionBehaviorReplace, callPresentation.Behavior)
	assert.Equal(t, llmstream.ErrorBehaviorDefault, callPresentation.ErrorBehavior)
	assert.Equal(t, callPresentation, resultPresentation)
	assert.Nil(t, callPresentation.Summary.Segments)

	diff, ok := callPresentation.Body.(llmstream.Diff)
	require.True(t, ok)
	assert.Equal(t, llmstream.Diff{
		Edits: []llmstream.DiffEdit{{
			Kind:    llmstream.DiffEditKindAdd,
			NewPath: "foo/new.txt",
			Lines: []llmstream.DiffLine{
				{Kind: llmstream.DiffLineKindAdd, Text: "first line"},
				{Kind: llmstream.DiffLineKindAdd, Text: "second line"},
			},
		}},
	}, diff)
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
			assert.Equal(t, llmstream.ErrorBehaviorDefault, callPresentation.ErrorBehavior)
			assert.Equal(t, callPresentation, resultPresentation)
			assert.Nil(t, callPresentation.Summary.Segments)

			diff, ok := callPresentation.Body.(llmstream.Diff)
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
	assert.Equal(t, llmstream.ErrorBehaviorDefault, presentation.ErrorBehavior)
	require.Len(t, presentation.Summary.Segments, 1)
	assert.Equal(t, "Apply Patch", presentation.Summary.Segments[0].Text)
	assert.Nil(t, presentation.Body)
}

func TestApplyPatchPresenter_ErrorOwnsPresentation(t *testing.T) {
	tool := NewApplyPatchTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), true, nil)
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	patch := `*** Begin Patch
*** Update File: foo/bar.go
@@
- old line
+ new line
*** End Patch
`

	presentation := presenter.Present(llmstream.ToolCall{
		Name:  ToolNameApplyPatch,
		Input: patch,
	}, &llmstream.ToolResult{
		Name:    ToolNameApplyPatch,
		Result:  "patch failed",
		IsError: true,
	})

	assert.Equal(t, llmstream.CompletionBehaviorReplace, presentation.Behavior)
	assert.Equal(t, llmstream.ErrorBehaviorPresenterOwned, presentation.ErrorBehavior)
	assert.Nil(t, presentation.Summary.Segments)

	diff, ok := presentation.Body.(llmstream.Diff)
	require.True(t, ok)
	require.Len(t, diff.Edits, 1)
	require.NotNil(t, diff.Edits[0].Error)
	require.Len(t, diff.Edits[0].Error.Segments, 1)
	assert.Equal(t, llmstream.RoleError, diff.Edits[0].Error.Segments[0].Role)
	assert.Equal(t, "Error: patch failed", diff.Edits[0].Error.Segments[0].Text)
}

func TestApplyPatchPresenter_InvalidPatchUsesBestEffortSummary(t *testing.T) {
	tool := NewApplyPatchTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), true, nil)
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	patch := `*** Update File: foo/bar.go
@@
- old line
+ new line
*** End Patch
`
	_, err := applypatch.ApplyPatch(t.TempDir(), patch)
	require.Error(t, err)
	require.True(t, applypatch.IsInvalidPatch(err))

	presentation := presenter.Present(llmstream.ToolCall{
		Name:  ToolNameApplyPatch,
		Input: patch,
	}, &llmstream.ToolResult{
		Name:      ToolNameApplyPatch,
		Result:    "invalid patch",
		IsError:   true,
		SourceErr: err,
	})

	assert.Equal(t, llmstream.ErrorBehaviorPresenterOwned, presentation.ErrorBehavior)
	assert.Nil(t, presentation.Summary.Segments)

	diff, ok := presentation.Body.(llmstream.Diff)
	require.True(t, ok)
	require.Len(t, diff.Edits, 1)
	assert.Empty(t, diff.Edits[0].Lines)
	require.NotNil(t, diff.Edits[0].Error)
	require.Len(t, diff.Edits[0].Error.Segments, 1)
	assert.Equal(t, "Failed: LLM supplied an invalid patch.", diff.Edits[0].Error.Segments[0].Text)
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
	assert.Nil(t, callPresentation.Body)
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
	assert.Nil(t, callPresentation.Body)
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
	assert.Nil(t, callPresentation.Body)
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
