package coretools

import (
	"github.com/codalotl/codalotl/internal/llmstream"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdatePlanInfo(t *testing.T) {
	tool := NewUpdatePlanTool("", nil)
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
	tool := NewUpdatePlanTool("", nil)
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
	tool := NewUpdatePlanTool("", nil)
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
	tool := NewUpdatePlanTool("", nil)
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
	tool := NewUpdatePlanTool("", nil)
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
