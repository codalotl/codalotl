package coretools

import (
	"context"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdatePlanInfo(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewUpdatePlanTool(auth)
	info := tool.Info()

	assert.Equal(t, ToolNameUpdatePlan, info.Name)
	assert.Contains(t, info.Description, "Updates the task plan")

	explanation, ok := info.Parameters["explanation"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", explanation["type"])

	plan, ok := info.Parameters["plan"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "array", plan["type"])

	items, ok := plan["items"].(map[string]any)
	require.True(t, ok)

	itemProps, ok := items["properties"].(map[string]any)
	require.True(t, ok)
	statusProp, ok := itemProps["status"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", statusProp["type"])

	enum, ok := statusProp["enum"].([]string)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"pending", "in_progress", "completed"}, enum)

	itemRequired, ok := items["required"].([]string)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"step", "status"}, itemRequired)

	assert.ElementsMatch(t, []string{"plan"}, info.Required)
}

func TestUpdatePlanRunSuccess(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewUpdatePlanTool(auth)
	call := llmstream.ToolCall{
		CallID: "call-success",
		Name:   ToolNameUpdatePlan,
		Type:   "function_call",
		Input:  `{"explanation":"Doing work","plan":[{"step":"Write code","status":"in_progress"}]}`,
	}

	res := tool.Run(context.Background(), call)

	assert.False(t, res.IsError)
	assert.Equal(t, "Plan updated", res.Result)
}

func TestUpdatePlanRunMissingPlan(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewUpdatePlanTool(auth)
	call := llmstream.ToolCall{
		CallID: "call-missing-plan",
		Name:   ToolNameUpdatePlan,
		Type:   "function_call",
		Input:  `{"explanation":"Nothing to do"}`,
	}

	res := tool.Run(context.Background(), call)

	assert.True(t, res.IsError)
	assert.Contains(t, res.Result, "plan is required")
}

func TestUpdatePlanRunMultipleInProgress(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewUpdatePlanTool(auth)
	call := llmstream.ToolCall{
		CallID: "call-multi-progress",
		Name:   ToolNameUpdatePlan,
		Type:   "function_call",
		Input:  `{"plan":[{"step":"First","status":"in_progress"},{"step":"Second","status":"in_progress"}]}`,
	}

	res := tool.Run(context.Background(), call)

	assert.True(t, res.IsError)
	assert.Contains(t, res.Result, "only one plan item")
}

func TestUpdatePlanRunInvalidStatus(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewUpdatePlanTool(auth)
	call := llmstream.ToolCall{
		CallID: "call-invalid-status",
		Name:   ToolNameUpdatePlan,
		Type:   "function_call",
		Input:  `{"plan":[{"step":"Do thing","status":"waiting"}]}`,
	}

	res := tool.Run(context.Background(), call)

	assert.True(t, res.IsError)
	assert.Contains(t, res.Result, "must be one of")
}

func TestUpdatePlanPresenter_CallRendersExplanationAndChecklist(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewUpdatePlanTool(auth)
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	presentation := presenter.Present(llmstream.ToolCall{
		Name:  ToolNameUpdatePlan,
		Input: `{"explanation":"Doing work","plan":[{"step":"Write code","status":"in_progress"}]}`,
	}, nil)

	assert.Equal(t, llmstream.CompletionBehaviorReplace, presentation.Behavior)
	assert.Equal(t, llmstream.Line{
		Segments: []llmstream.Segment{
			{Text: "Update Plan", Role: llmstream.RoleAction},
		},
	}, presentation.Summary)
	require.Len(t, presentation.Body, 2)

	explanation, ok := presentation.Body[0].(llmstream.Paragraph)
	require.True(t, ok)
	require.Len(t, explanation.Lines, 1)
	assert.Equal(t, llmstream.Line{
		Segments: []llmstream.Segment{
			{Text: "Doing work", Role: llmstream.RoleAccent},
		},
	}, explanation.Lines[0])

	checklist, ok := presentation.Body[1].(llmstream.Checklist)
	require.True(t, ok)
	require.Len(t, checklist.Items, 1)
	assert.Equal(t, llmstream.ChecklistItem{
		Status: llmstream.ChecklistStatusInProgress,
		Line: llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: "Write code", Role: llmstream.RoleAction},
			},
		},
	}, checklist.Items[0])
}

func TestUpdatePlanPresenter_CompleteRendersExplanationAndChecklistEmphasis(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewUpdatePlanTool(auth)
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name: ToolNameUpdatePlan,
		Input: `{
  "explanation": "Need to align tool rendering with presenter output.",
  "plan": [
    {"step":"Review the existing formatting shapes","status":"completed"},
    {"step":"Highlight the first pending task as next-up work","status":"pending"},
    {"step":"Keep explicit in-progress items emphasized too","status":"in_progress"},
    {"step":"Follow up with cleanup later","status":"pending"}
  ]
}`,
	}

	presentation := presenter.Present(call, &llmstream.ToolResult{Name: ToolNameUpdatePlan, Result: "Plan updated"})

	assert.Equal(t, llmstream.CompletionBehaviorReplace, presentation.Behavior)
	assert.Equal(t, llmstream.Line{
		Segments: []llmstream.Segment{
			{Text: "Update Plan", Role: llmstream.RoleAction},
		},
	}, presentation.Summary)

	require.Len(t, presentation.Body, 2)

	explanation, ok := presentation.Body[0].(llmstream.Paragraph)
	require.True(t, ok)
	require.Len(t, explanation.Lines, 1)
	assert.Equal(t, llmstream.Line{
		Segments: []llmstream.Segment{
			{Text: "Need to align tool rendering with presenter output.", Role: llmstream.RoleAccent},
		},
	}, explanation.Lines[0])

	checklist, ok := presentation.Body[1].(llmstream.Checklist)
	require.True(t, ok)
	require.Len(t, checklist.Items, 4)

	assert.Equal(t, llmstream.ChecklistItem{
		Status: llmstream.ChecklistStatusCompleted,
		Line: llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: "Review the existing formatting shapes", Role: llmstream.RoleAccent},
			},
		},
	}, checklist.Items[0])
	assert.Equal(t, llmstream.ChecklistItem{
		Status: llmstream.ChecklistStatusPending,
		Line: llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: "Highlight the first pending task as next-up work", Role: llmstream.RoleAction},
			},
		},
	}, checklist.Items[1])
	assert.Equal(t, llmstream.ChecklistItem{
		Status: llmstream.ChecklistStatusInProgress,
		Line: llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: "Keep explicit in-progress items emphasized too", Role: llmstream.RoleAction},
			},
		},
	}, checklist.Items[2])
	assert.Equal(t, llmstream.ChecklistItem{
		Status: llmstream.ChecklistStatusPending,
		Line: llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: "Follow up with cleanup later", Role: llmstream.RoleAccent},
			},
		},
	}, checklist.Items[3])
}
