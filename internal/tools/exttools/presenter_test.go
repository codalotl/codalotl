package exttools

import (
	"testing"

	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	_ llmstream.Tool = (*toolDiagnostics)(nil)
	_ llmstream.Tool = (*toolFixLints)(nil)
	_ llmstream.Tool = (*toolRunProjectTests)(nil)
	_ llmstream.Tool = (*toolRunTests)(nil)
)

func TestTools_ExposePresenters(t *testing.T) {
	auth := authdomain.NewAutoApproveAuthorizer(t.TempDir())

	tools := []llmstream.Tool{
		NewDiagnosticsTool(auth),
		NewFixLintsTool(auth, nil),
		NewRunProjectTestsTool("", auth),
		NewRunTestsTool(auth, nil),
	}

	for _, tool := range tools {
		assert.NotNil(t, tool.Presenter())
	}
}

func TestPresenters_SubagentEventPolicy_Default(t *testing.T) {
	auth := authdomain.NewAutoApproveAuthorizer(t.TempDir())

	tools := []llmstream.Tool{
		NewDiagnosticsTool(auth),
		NewFixLintsTool(auth, nil),
		NewRunProjectTestsTool("", auth),
		NewRunTestsTool(auth, nil),
	}

	for _, tool := range tools {
		assert.Equal(t, llmstream.SubagentEventPolicyDefault, tool.Presenter().SubagentEventPolicy(llmstream.ToolCall{
			Name: tool.Name(),
		}))
	}
}

func TestDiagnosticsPresenter(t *testing.T) {
	tool := NewDiagnosticsTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  ToolNameDiagnostics,
		Input: `{"path":"./internal/agentformatter"}`,
	}
	result := &llmstream.ToolResult{
		Name:   ToolNameDiagnostics,
		Result: `<diagnostics-status ok="false">$ go build ./internal/agentformatter</diagnostics-status>`,
	}

	callPresentation := presenter.Present(call, nil)
	resultPresentation := presenter.Present(call, result)

	assert.Equal(t, llmstream.CompletionBehaviorReplace, callPresentation.Behavior)
	assert.Equal(t, llmstream.CompletionBehaviorReplace, resultPresentation.Behavior)
	assert.Equal(t, llmstream.Line{
		JoinWithSpace: true,
		Segments: []llmstream.Segment{
			{Text: "Run Diagnostics", Role: llmstream.RoleAction},
			{Text: "./internal/agentformatter", Role: llmstream.RoleNormal},
		},
	}, callPresentation.Summary)
	assert.Equal(t, llmstream.Line{
		JoinWithSpace: true,
		Segments: []llmstream.Segment{
			{Text: "Ran Diagnostics", Role: llmstream.RoleAction},
			{Text: "./internal/agentformatter", Role: llmstream.RoleNormal},
		},
	}, resultPresentation.Summary)
	assert.Equal(t, llmstream.ErrorBehaviorPresenterOwned, resultPresentation.ErrorBehavior)
	assert.Nil(t, resultPresentation.Body)
}

func TestFixLintsPresenter(t *testing.T) {
	tool := NewFixLintsTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), nil)
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	presentation := presenter.Present(llmstream.ToolCall{
		Name:  ToolNameFixLints,
		Input: `{"path":"./internal/agentformatter"}`,
	}, &llmstream.ToolResult{
		Name: ToolNameFixLints,
		Result: `<lint-status ok="true">
<command ok="true">
$ gofmt -l -w internal/agentformatter
</command>
<command ok="true">
internal/agentformatter/agentformatter.go
</command>
</lint-status>`,
	})

	assert.Equal(t, llmstream.Line{
		JoinWithSpace: true,
		Segments: []llmstream.Segment{
			{Text: "Fixed Lints", Role: llmstream.RoleAction},
			{Text: "./internal/agentformatter", Role: llmstream.RoleNormal},
		},
	}, presentation.Summary)

	output, ok := presentation.Body.(llmstream.Output)
	require.True(t, ok)
	assert.Equal(t, llmstream.Output{
		Lines: []string{
			"$ gofmt -l -w internal/agentformatter",
			"internal/agentformatter/agentformatter.go",
		},
	}, output)
}

func TestRunTestsPresenter_UsesSectionStatusSummary(t *testing.T) {
	tool := NewRunTestsTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), nil)
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	presentation := presenter.Present(llmstream.ToolCall{
		Name:  ToolNameRunTests,
		Input: `{"path":"./internal/tools/toolsets"}`,
	}, &llmstream.ToolResult{
		Name: ToolNameRunTests,
		Result: `<test-status ok="true">
$ go test ./internal/tools/toolsets
</test-status>
<lint-status ok="false">
$ gofmt -l -w internal/tools/toolsets
</lint-status>`,
	})

	assert.Equal(t, llmstream.PresentationStatusFailure, presentation.Status)
	paragraph, ok := presentation.Body.(llmstream.Paragraph)
	require.True(t, ok)
	require.Len(t, paragraph.Lines, 1)
	assert.Equal(t, llmstream.Line{
		Segments: []llmstream.Segment{
			{Text: "Tests: pass | Lints: fail", Role: llmstream.RoleAccent},
		},
	}, paragraph.Lines[0])
}

func TestRunProjectTestsPresenter(t *testing.T) {
	tool := NewRunProjectTestsTool("", authdomain.NewAutoApproveAuthorizer(t.TempDir()))
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	presentation := presenter.Present(llmstream.ToolCall{
		Name:  ToolNameRunProjectTests,
		Input: `{}`,
	}, &llmstream.ToolResult{
		Name:   ToolNameRunProjectTests,
		Result: `{"success":false,"content":"Failed:\nsome/pkg1\nother/pkg2\n"}`,
	})

	assert.Equal(t, llmstream.ErrorBehaviorPresenterOwned, presentation.ErrorBehavior)
	paragraph, ok := presentation.Body.(llmstream.Paragraph)
	require.True(t, ok)
	assert.Equal(t, llmstream.Paragraph{
		Lines: []llmstream.Line{
			{Segments: []llmstream.Segment{{Text: "Failed:", Role: llmstream.RoleAccent}}},
			{Segments: []llmstream.Segment{{Text: "some/pkg1", Role: llmstream.RoleAccent}}},
			{Segments: []llmstream.Segment{{Text: "other/pkg2", Role: llmstream.RoleAccent}}},
		},
	}, paragraph)
}
