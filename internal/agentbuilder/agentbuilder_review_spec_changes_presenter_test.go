package agentbuilder

import (
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRegistry_PROrchestratorReviewSpecChangesTool_ExposesPresenter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	reviewSpecTool := requireTool(t, invokeAgentTools(
		t,
		"pr-orchestrator",
		llmmodel.ProviderIDOpenAI.DefaultModel(),
		t.TempDir(),
		"",
		nil,
	), "review_spec_changes")

	presenter := reviewSpecTool.Presenter()
	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  "review_spec_changes",
		Input: `{"package":"internal/agentbuilder","message":"I updated SPEC.md for the new review loop. Check whether it stays terse and avoids over-specifying implementation details."}`,
	}
	result := &llmstream.ToolResult{
		Name:   "review_spec_changes",
		Result: "Looks directionally right. Consider removing one sentence that repeats an obvious implementation detail.",
	}

	callPresentation := presenter.Present(call, nil)
	resultPresentation := presenter.Present(call, result)

	assert.Equal(t, llmstream.Presentation{
		Behavior:      llmstream.CompletionBehaviorAppend,
		ErrorBehavior: llmstream.ErrorBehaviorDefault,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Reviewing SPEC changes in", Role: llmstream.RoleAction},
				{Text: "internal/agentbuilder", Role: llmstream.RoleNormal},
			},
		},
		Body: llmstream.Output{
			Lines: []string{"I updated SPEC.md for the new review loop. Check whether it stays terse and avoids over-specifying implementation details."},
		},
	}, callPresentation)

	assert.Equal(t, llmstream.Presentation{
		Behavior:      llmstream.CompletionBehaviorAppend,
		ErrorBehavior: llmstream.ErrorBehaviorDefault,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Reviewed SPEC changes in", Role: llmstream.RoleAction},
				{Text: "internal/agentbuilder", Role: llmstream.RoleNormal},
			},
		},
		Body: llmstream.Output{
			Lines: []string{"Looks directionally right. Consider removing one sentence that repeats an obvious implementation detail."},
		},
	}, resultPresentation)
}

func TestBuildRegistry_PROrchestratorReviewSpecChangesTool_PresenterSuppressesSubagentFinalMessage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	reviewSpecTool := requireTool(t, invokeAgentTools(
		t,
		"pr-orchestrator",
		llmmodel.ProviderIDOpenAI.DefaultModel(),
		t.TempDir(),
		"",
		nil,
	), "review_spec_changes")

	presenter := reviewSpecTool.Presenter()
	require.NotNil(t, presenter)

	finalMessagePresenter, ok := presenter.(llmstream.SubagentFinalMessagePresenter)
	require.True(t, ok)
	assert.Nil(t, finalMessagePresenter.SubagentFinalMessage(llmstream.ToolCall{
		Name:  "review_spec_changes",
		Input: `{"package":"internal/agentbuilder","message":"Review the latest SPEC edits."}`,
	}, "spec review worker", "done"))
}
