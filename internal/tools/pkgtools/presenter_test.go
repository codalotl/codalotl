package pkgtools

import (
	"encoding/json"
	"testing"

	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadOnlyTools_ExposePresenters(t *testing.T) {
	auth := authdomain.NewAutoApproveAuthorizer(t.TempDir())

	tools := []llmstream.Tool{
		NewChangeAPITool(".", auth, nil, "", nil),
		NewClarifyPublicAPITool(auth, nil),
		NewGetPublicAPITool(auth),
		NewGetUsageTool(auth),
		NewModuleInfoTool(auth),
		NewUpdateUsageTool(".", auth, nil, "", nil),
	}

	for _, tool := range tools {
		assert.NotNil(t, tool.Presenter())
	}
}

func TestPresenters_SubagentEventPolicy(t *testing.T) {
	auth := authdomain.NewAutoApproveAuthorizer(t.TempDir())
	call := llmstream.ToolCall{Name: "test"}

	policyByTool := map[llmstream.Tool]llmstream.SubagentEventPolicy{
		NewChangeAPITool(".", auth, nil, "", nil):   llmstream.SubagentEventPolicyHideFinalMessage,
		NewClarifyPublicAPITool(auth, nil):          llmstream.SubagentEventPolicyHideFinalMessage,
		NewGetPublicAPITool(auth):                   llmstream.SubagentEventPolicyDefault,
		NewGetUsageTool(auth):                       llmstream.SubagentEventPolicyDefault,
		NewModuleInfoTool(auth):                     llmstream.SubagentEventPolicyDefault,
		NewUpdateUsageTool(".", auth, nil, "", nil): llmstream.SubagentEventPolicyHideFinalMessage,
	}

	for tool, policy := range policyByTool {
		assert.Equal(t, policy, tool.Presenter().SubagentEventPolicy(call))
	}
}

func TestGetPublicAPIPresenter(t *testing.T) {
	tool := NewGetPublicAPITool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  ToolNameGetPublicAPI,
		Input: `{"path":"axi/some/pkg","identifiers":["SomeType","DoThingFunc"]}`,
	}
	result := &llmstream.ToolResult{
		Name:   ToolNameGetPublicAPI,
		Result: `{"success":true}`,
	}

	callPresentation := presenter.Present(call, nil)
	resultPresentation := presenter.Present(call, result)

	expectedSummary := llmstream.Line{
		JoinWithSpace: true,
		Segments: []llmstream.Segment{
			{Text: "Read Public API", Role: llmstream.RoleAction},
			{Text: "axi/some/pkg", Role: llmstream.RoleNormal},
		},
	}
	expectedBody := llmstream.Output{
		Lines: []string{"SomeType, DoThingFunc"},
	}

	assert.Equal(t, llmstream.CompletionBehaviorReplace, callPresentation.Behavior)
	assert.Equal(t, llmstream.PresentationNarrowBehaviorPreferCLI, callPresentation.NarrowBehavior)
	assert.Equal(t, expectedSummary, callPresentation.Summary)
	assert.Equal(t, expectedSummary, resultPresentation.Summary)
	assert.Equal(t, expectedBody, callPresentation.Body)
	assert.Equal(t, expectedBody, resultPresentation.Body)
}

func TestGetUsagePresenter(t *testing.T) {
	tool := NewGetUsageTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  ToolNameGetUsage,
		Input: `{"defining_package_path":"axi/some/pkg","identifier":"*SomeType.SomeFunc"}`,
	}
	payload, err := json.Marshal(map[string]any{
		"success": true,
		"content": "1: first\nSome details\n2: second\n3: third",
	})
	require.NoError(t, err)
	result := &llmstream.ToolResult{
		Name:   ToolNameGetUsage,
		Result: string(payload),
	}

	callPresentation := presenter.Present(call, nil)
	resultPresentation := presenter.Present(call, result)

	assert.Equal(t, llmstream.CompletionBehaviorReplace, callPresentation.Behavior)
	assert.Equal(t, llmstream.PresentationNarrowBehaviorPreferCLI, callPresentation.NarrowBehavior)
	assert.Equal(t, llmstream.Line{
		JoinWithSpace: true,
		Segments: []llmstream.Segment{
			{Text: "Read Usage", Role: llmstream.RoleAction},
			{Text: "axi/some/pkg", Role: llmstream.RoleNormal},
			{Text: "*SomeType.SomeFunc", Role: llmstream.RoleNormal},
		},
	}, callPresentation.Summary)
	assert.Nil(t, callPresentation.Body)
	assert.Equal(t, llmstream.Paragraph{
		Lines: []llmstream.Line{{
			Segments: []llmstream.Segment{
				{Text: "Found 3 results.", Role: llmstream.RoleAccent},
			},
		}},
	}, resultPresentation.Body)
}

func TestGetUsagePresenter_FallsBackToToolNameWhenPathMissing(t *testing.T) {
	tool := NewGetUsageTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	presentation := presenter.Present(llmstream.ToolCall{
		Name:  ToolNameGetUsage,
		Input: `{"path":"axi/some/pkg","identifier":"*SomeType.SomeFunc"}`,
	}, nil)

	assert.Equal(t, llmstream.Line{
		JoinWithSpace: true,
		Segments: []llmstream.Segment{
			{Text: "Read Usage", Role: llmstream.RoleAction},
			{Text: ToolNameGetUsage, Role: llmstream.RoleNormal},
			{Text: "*SomeType.SomeFunc", Role: llmstream.RoleNormal},
		},
	}, presentation.Summary)
}

func TestModuleInfoPresenter(t *testing.T) {
	tool := NewModuleInfoTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  ToolNameModuleInfo,
		Input: `{"package_search":"agentformatter","include_dependency_packages":true}`,
	}
	result := &llmstream.ToolResult{
		Name:   ToolNameModuleInfo,
		Result: `{"success":true,"content":"(big payload elided)"}`,
	}

	callPresentation := presenter.Present(call, nil)
	resultPresentation := presenter.Present(call, result)

	expectedSummary := llmstream.Line{
		JoinWithSpace: true,
		Segments: []llmstream.Segment{
			{Text: "Read Module Info", Role: llmstream.RoleAction},
		},
	}
	expectedBody := llmstream.Paragraph{
		Lines: []llmstream.Line{{
			Segments: []llmstream.Segment{
				{Text: "Search: agentformatter; Deps: true", Role: llmstream.RoleAccent},
			},
		}},
	}

	assert.Equal(t, llmstream.CompletionBehaviorReplace, callPresentation.Behavior)
	assert.Equal(t, llmstream.PresentationNarrowBehaviorPreferCLI, callPresentation.NarrowBehavior)
	assert.Equal(t, expectedSummary, callPresentation.Summary)
	assert.Equal(t, expectedSummary, resultPresentation.Summary)
	assert.Equal(t, expectedBody, callPresentation.Body)
	assert.Equal(t, expectedBody, resultPresentation.Body)
	assert.Equal(t, llmstream.ErrorBehaviorDefault, resultPresentation.ErrorBehavior)
}
