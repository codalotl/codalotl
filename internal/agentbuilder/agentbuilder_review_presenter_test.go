package agentbuilder

import (
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRegistry_PROrchestratorReviewTool_ExposesPresenter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	reviewTool := requireTool(t, invokeAgentTools(
		t,
		"pr-orchestrator",
		llmmodel.ProviderIDOpenAI.DefaultModel(),
		t.TempDir(),
		"",
		nil,
	), "review")

	presenter := reviewTool.Presenter()
	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  "review",
		Input: `{"base":"origin/main"}`,
	}
	result := &llmstream.ToolResult{
		Name: "review",
		Result: `{
			"findings": [
				{
					"title": "[P1] Preserve machine-readable review output",
					"body": "The orchestrator expects structured JSON back from review.",
					"confidence_score": 0.92,
					"priority": 1,
					"code_location": {
						"absolute_file_path": "/tmp/review.go",
						"line_range": {"start": 1, "end": 1}
					}
				}
			],
			"overall_correctness": "patch is incorrect",
			"overall_explanation": "One actionable issue remains.",
			"overall_confidence_score": 0.92
		}`,
	}

	callPresentation := presenter.Present(call, nil)
	resultPresentation := presenter.Present(call, result)

	assert.Equal(t, llmstream.Presentation{
		Behavior:      llmstream.CompletionBehaviorAppend,
		ErrorBehavior: llmstream.ErrorBehaviorDefault,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Reviewing", Role: llmstream.RoleAction},
				{Text: "origin/main", Role: llmstream.RoleNormal},
			},
		},
	}, callPresentation)

	assert.Equal(t, llmstream.Presentation{
		Behavior:      llmstream.CompletionBehaviorAppend,
		ErrorBehavior: llmstream.ErrorBehaviorDefault,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Reviewed", Role: llmstream.RoleAction},
				{Text: "origin/main", Role: llmstream.RoleNormal},
			},
		},
		Body: llmstream.Output{
			Lines: []string{"[P1] Preserve machine-readable review output"},
		},
	}, resultPresentation)
}

func TestBuildRegistry_PROrchestratorReviewTool_PresenterHidesSubagentFinalMessage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	reviewTool := requireTool(t, invokeAgentTools(
		t,
		"pr-orchestrator",
		llmmodel.ProviderIDOpenAI.DefaultModel(),
		t.TempDir(),
		"",
		nil,
	), "review")

	presenter := reviewTool.Presenter()
	require.NotNil(t, presenter)

	assert.Equal(t, llmstream.SubagentEventPolicyHideFinalMessage, presenter.SubagentEventPolicy(llmstream.ToolCall{
		Name:  "review",
		Input: `{"base":"origin/main"}`,
	}))
}
