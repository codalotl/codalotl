package coretools

import (
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/auth"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const ToolNameUpdatePlan = "update_plan"

// toolUpdatePlan communicates plan updates to the harness. It does not read or write files.
type toolUpdatePlan struct {
	sandboxAbsDir string
	authorizer    auth.Authorizer
}

func NewUpdatePlanTool(sandboxAbsDir string, authorizer auth.Authorizer) llmstream.Tool {
	return &toolUpdatePlan{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
	}
}

func (t *toolUpdatePlan) Name() string {
	return ToolNameUpdatePlan
}

func (t *toolUpdatePlan) Info() llmstream.ToolInfo {
	// The schema mirrors the desired plan format. Do not load or embed external schema files.
	return llmstream.ToolInfo{
		Name:        ToolNameUpdatePlan,
		Description: strings.TrimSpace("Updates the task plan.\nProvide an optional explanation and a list of plan items, each with a step and status.\nAt most one step can be in_progress at a time.\n"),
		Parameters: map[string]any{
			"explanation": map[string]any{
				"type": "string",
			},
			"plan": map[string]any{
				"type":        "array",
				"description": "The list of steps",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"step": map[string]any{
							"type": "string",
						},
						"status": map[string]any{
							"type":        "string",
							"description": "One of: pending, in_progress, completed",
							"enum":        []string{"pending", "in_progress", "completed"},
						},
					},
					"required":             []string{"step", "status"},
					"additionalProperties": false,
				},
			},
		},
		Required: []string{"plan"},
	}
}

type updatePlanItem struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

type updatePlanParams struct {
	Explanation string           `json:"explanation"`
	Plan        []updatePlanItem `json:"plan"`
}

func (t *toolUpdatePlan) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	var params updatePlanParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	// plan is required (presence, not necessarily non-empty)
	if params.Plan == nil {
		return llmstream.NewErrorToolResult("plan is required", call)
	}

	allowed := map[string]struct{}{
		"pending":     {},
		"in_progress": {},
		"completed":   {},
	}
	inProgressCount := 0

	for i, it := range params.Plan {
		// Basic validation of required fields
		if strings.TrimSpace(it.Step) == "" {
			return llmstream.NewErrorToolResult(fmt.Sprintf("plan[%d].step is required", i), call)
		}
		if strings.TrimSpace(it.Status) == "" {
			return llmstream.NewErrorToolResult(fmt.Sprintf("plan[%d].status is required", i), call)
		}
		if _, ok := allowed[it.Status]; !ok {
			return llmstream.NewErrorToolResult("status must be one of: pending, in_progress, completed", call)
		}
		if it.Status == "in_progress" {
			inProgressCount++
		}
	}

	if inProgressCount > 1 {
		return llmstream.NewErrorToolResult("only one plan item may be in_progress at a time", call)
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: "Plan updated",
	}
}
