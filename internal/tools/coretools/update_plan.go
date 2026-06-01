package coretools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"strings"
)

// ToolNameUpdatePlan is the registered name of the update_plan tool.
const ToolNameUpdatePlan = "update_plan"

// toolUpdatePlan communicates plan updates to the harness. It does not read or write files.
type toolUpdatePlan struct {
	sandboxAbsDir string                // This is the absolute sandbox root associated with the tool session.
	authorizer    authdomain.Authorizer // This is the session authorizer; update_plan does not use it for filesystem access.
}

// NewUpdatePlanTool returns an update_plan tool that communicates plan updates to the harness without reading or writing files. The authorizer must be non-nil.
func NewUpdatePlanTool(authorizer authdomain.Authorizer) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolUpdatePlan{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
	}
}

// Name returns ToolNameUpdatePlan, the registered name of the update_plan tool.
func (t *toolUpdatePlan) Name() string {
	return ToolNameUpdatePlan
}

// Presenter returns the semantic checklist presenter for update_plan tool calls and results.
func (t *toolUpdatePlan) Presenter() llmstream.Presenter {
	return updatePlanPresenterInstance
}

// Info returns the tool metadata for update_plan. The returned schema requires a plan array of step/status items, accepts an optional explanation, and restricts
// statuses to pending, in_progress, or completed.
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

// An updatePlanItem is one step in an update_plan request.
type updatePlanItem struct {
	Step   string `json:"step"`   // This is the user-facing description of the plan step.
	Status string `json:"status"` // This is the step state: "pending", "in_progress", or "completed".
}

// updatePlanParams contains the JSON arguments for update_plan. Plan must be present. Each item must have a non-empty step and a status of "pending", "in_progress",
// or "completed", with at most one "in_progress" item.
type updatePlanParams struct {
	Explanation string           `json:"explanation"` // Explanation is optional text shown above the checklist when non-empty.
	Plan        []updatePlanItem `json:"plan"`        // Plan is the ordered checklist of plan items and may be empty.
}

var updatePlanPresenterInstance llmstream.Presenter = updatePlanPresenter{}

// updatePlanPresenter formats update_plan calls as a replacing presentation with an "Update Plan" summary and checklist body.
type updatePlanPresenter struct{}

// Present returns a replacement presentation for an update_plan call with an "Update Plan" summary. Valid JSON input is rendered as a checklist body with an optional
// explanation; invalid input returns the summary only.
func (p updatePlanPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	presentation := llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: "Update Plan", Role: llmstream.RoleAction},
			},
		},
	}

	var params updatePlanParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return presentation
	}

	presentation.Body = updatePlanPresenterBody(params)
	if result == nil {
		return presentation
	}

	return presentation
}

// The updatePlanPresenterBody function builds the checklist body for an update_plan presentation. It includes a nonblank explanation as the checklist overview,
// preserves plan order, and highlights the first unfinished item and any in-progress items. It returns nil when there is nothing to present.
func updatePlanPresenterBody(params updatePlanParams) llmstream.Block {
	items := make([]llmstream.ChecklistItem, 0, len(params.Plan))
	nextUpIndex := updatePlanNextUpIndex(params.Plan)
	for i, item := range params.Plan {
		role := llmstream.RoleAccent
		if i == nextUpIndex || item.Status == "in_progress" {
			role = llmstream.RoleAction
		}

		items = append(items, llmstream.ChecklistItem{
			Status: updatePlanChecklistStatus(item.Status),
			Line: llmstream.Line{
				Segments: []llmstream.Segment{
					{Text: item.Step, Role: role},
				},
			},
		})
	}

	checklist := llmstream.Checklist{
		Items: items,
	}
	if strings.TrimSpace(params.Explanation) != "" {
		checklist.Overview = llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: params.Explanation, Role: llmstream.RoleAccent},
			},
		}
	}
	if len(checklist.Overview.Segments) == 0 && len(checklist.Items) == 0 {
		return nil
	}

	return checklist
}

func updatePlanNextUpIndex(plan []updatePlanItem) int {
	for i, item := range plan {
		if item.Status != "completed" {
			return i
		}
	}
	return -1
}

func updatePlanChecklistStatus(status string) llmstream.ChecklistStatus {
	switch status {
	case "completed":
		return llmstream.ChecklistStatusCompleted
	case "in_progress":
		return llmstream.ChecklistStatusInProgress
	default:
		return llmstream.ChecklistStatusPending
	}
}

// Run validates and accepts an update_plan request. It requires a present plan array, allows an empty plan, enforces non-empty steps with pending, in_progress,
// or completed statuses, and rejects more than one in_progress item.
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
