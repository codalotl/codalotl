package agentbuilder

import (
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRegistry_PROrchestratorImplementTool_ExposesResultOnCompletionBody(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	implementTool := requireTool(t, invokeAgentTools(
		t,
		"pr-orchestrator",
		llmmodel.ProviderIDOpenAI.DefaultModel(),
		t.TempDir(),
		"",
		nil,
	), "implement")

	presenter := implementTool.Presenter()
	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  "implement",
		Input: `{"path":"internal/agentbuilder","instructions":"Refine the presenter UX."}`,
	}
	result := &llmstream.ToolResult{
		Name:   "implement",
		Result: "Implemented the presenter UX refinement.\nUpdated focused tests.",
	}

	callPresentation := presenter.Present(call, nil)
	resultPresentation := presenter.Present(call, result)

	assert.Equal(t, llmstream.Presentation{
		Behavior:      llmstream.CompletionBehaviorAppend,
		ErrorBehavior: llmstream.ErrorBehaviorDefault,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Implementing", Role: llmstream.RoleAction},
				{Text: "internal/agentbuilder", Role: llmstream.RoleNormal},
			},
		},
		Body: llmstream.Output{
			Lines: []string{"Refine the presenter UX."},
		},
	}, callPresentation)

	assert.Equal(t, llmstream.Presentation{
		Behavior:      llmstream.CompletionBehaviorAppend,
		ErrorBehavior: llmstream.ErrorBehaviorDefault,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Implemented", Role: llmstream.RoleAction},
				{Text: "internal/agentbuilder", Role: llmstream.RoleNormal},
			},
		},
		Body: llmstream.Output{
			Lines: []string{
				"Implemented the presenter UX refinement.",
				"Updated focused tests.",
			},
		},
	}, resultPresentation)
}

func TestBuildRegistry_PROrchestratorImplementTool_PresenterHidesSubagentFinalMessage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	implementTool := requireTool(t, invokeAgentTools(
		t,
		"pr-orchestrator",
		llmmodel.ProviderIDOpenAI.DefaultModel(),
		t.TempDir(),
		"",
		nil,
	), "implement")

	presenter := implementTool.Presenter()
	require.NotNil(t, presenter)

	assert.Equal(t, llmstream.SubagentEventPolicyHideFinalMessage, presenter.SubagentEventPolicy(llmstream.ToolCall{
		Name:  "implement",
		Input: `{"path":"internal/agentbuilder","instructions":"Refine the presenter UX."}`,
	}))
}
