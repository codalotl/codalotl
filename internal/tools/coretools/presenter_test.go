package coretools

import (
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
		NewApplyPatchTool(auth, false, nil),
		NewApplyPatchTool(auth, true, nil),
		NewDeleteTool(auth),
		NewEditTool(auth),
		NewSkillShellTool(auth),
		NewUpdatePlanTool(auth),
		NewWriteTool(auth),
	}

	for _, tool := range tools {
		assert.Nil(t, tool.Presenter())
	}
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
