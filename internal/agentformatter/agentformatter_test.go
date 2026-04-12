package agentformatter

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/applypatch"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/termformat"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/exttools"
	"github.com/codalotl/codalotl/internal/tools/pkgtools"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var dedent = gocodetesting.Dedent

func extToolWithPresenter(t *testing.T, tool llmstream.Tool) llmstream.Tool {
	t.Helper()
	require.NotNil(t, tool.Presenter())
	return testToolWithPresenter(tool.Name(), tool.Presenter())
}

func pkgToolWithPresenter(t *testing.T, tool llmstream.Tool) llmstream.Tool {
	t.Helper()
	require.NotNil(t, tool.Presenter())
	return testToolWithPresenter(tool.Name(), tool.Presenter())
}

func ansiWrap(text string, pal palette, c colorRole, italics bool, bold bool) string {
	style := pal.style(runeStyle{
		color:  c,
		italic: italics,
		bold:   bold,
	})
	return style.Wrap(text)
}

type staticPresenter struct {
	call     llmstream.Presentation
	complete llmstream.Presentation
}

func (p staticPresenter) Present(_ llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	if result == nil {
		return p.call
	}
	return p.complete
}

func presentedReplaceSummary(action, target string) llmstream.Presentation {
	segments := []llmstream.Segment{{
		Text: action,
		Role: llmstream.RoleAction,
	}}
	if target != "" {
		segments = append(segments, llmstream.Segment{
			Text: " " + target,
			Role: llmstream.RoleNormal,
		})
	}
	return llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			Segments: segments,
		},
	}
}

func presentedReplaceLine(line llmstream.Line) llmstream.Presentation {
	return llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary:  line,
	}
}

func presentedReplaceSummaryWithOutput(action, target string, output llmstream.Output) llmstream.Presentation {
	presentation := presentedReplaceSummary(action, target)
	presentation.Body = output
	return presentation
}

func presentedReplaceSummaryWithBody(action, target string, body llmstream.Block) llmstream.Presentation {
	presentation := presentedReplaceSummary(action, target)
	switch body.(type) {
	case llmstream.Diff, *llmstream.Diff:
		presentation.Summary = llmstream.Line{}
	}
	presentation.Body = body
	return presentation
}

func newUpdatePlanTool(t *testing.T) llmstream.Tool {
	t.Helper()
	return coretools.NewUpdatePlanTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
}

func newDeleteTool(t *testing.T) llmstream.Tool {
	t.Helper()
	return coretools.NewDeleteTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
}

func newEditTool(t *testing.T) llmstream.Tool {
	t.Helper()
	return coretools.NewEditTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
}

func newWriteTool(t *testing.T) llmstream.Tool {
	t.Helper()
	return coretools.NewWriteTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
}

func newApplyPatchTool(t *testing.T) llmstream.Tool {
	t.Helper()
	return coretools.NewApplyPatchTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), true, nil)
}

func newClarifyPublicAPITool(t *testing.T) llmstream.Tool {
	t.Helper()
	return pkgtools.NewClarifyPublicAPITool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), nil)
}

func newUpdateUsageTool(t *testing.T) llmstream.Tool {
	t.Helper()
	sandbox := t.TempDir()
	return pkgtools.NewUpdateUsageTool(sandbox, authdomain.NewAutoApproveAuthorizer(sandbox), nil, llmmodel.DefaultModel, nil)
}

func TestAgentMessageTableDriven(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)
	pal := newPalette(cfg)

	testCases := []struct {
		name     string
		message  string
		tuiWidth int
		expected string
	}{
		{
			name:     "short message",
			message:  "The answer is 9.",
			tuiWidth: 60,
			expected: dedent(`
                • The answer is 9.
			`),
		},
		{
			name:     "basic paragraph",
			message:  "I'll explain that Codex uses the full pulldown-cmark Markdown parser combined with targeted regex rewriting for specific citation patterns like 【F:...】, which get converted into clickable links. Inline code marked by backticks is only styled as dim text, not linkified, and the linking mechanics rely on actual Markdown links or those citations rather than scanning inline code for paths. I'll reference key code locations that handle these steps for clarity.",
			tuiWidth: 120,
			expected: dedent(`
                • I'll explain that Codex uses the full pulldown-cmark Markdown parser combined with targeted regex rewriting for
                  specific citation patterns like 【F:...】, which get converted into clickable links. Inline code marked by backticks
                  is only styled as dim text, not linkified, and the linking mechanics rely on actual Markdown links or those citations
                  rather than scanning inline code for paths. I'll reference key code locations that handle these steps for clarity.
			`),
		},
		{
			name:     "basic paragraph - exact fit",
			message:  "I'll explain that Codex uses the full pulldown-cmark Markdown parser combined with targeted regex rewriting for specific citation patterns like 【F:...】, which get converted into clickable links. Inline code marked by backticks is only styled as dim text, not linkified, and the linking mechanics rely on actual Markdown links or those citations rather than scanning inline code for paths. I'll reference key code locations that handle these steps for clarity.",
			tuiWidth: 119,
			expected: dedent(`
                • I'll explain that Codex uses the full pulldown-cmark Markdown parser combined with targeted regex rewriting for
                  specific citation patterns like 【F:...】, which get converted into clickable links. Inline code marked by backticks
                  is only styled as dim text, not linkified, and the linking mechanics rely on actual Markdown links or those citations
                  rather than scanning inline code for paths. I'll reference key code locations that handle these steps for clarity.
			`),
		},
		{
			name:     "basic paragraph - reflow",
			message:  "I'll explain that Codex uses the full pulldown-cmark Markdown parser combined with targeted regex rewriting for specific citation patterns like 【F:...】, which get converted into clickable links. Inline code marked by backticks is only styled as dim text, not linkified, and the linking mechanics rely on actual Markdown links or those citations rather than scanning inline code for paths. I'll reference key code locations that handle these steps for clarity.",
			tuiWidth: 118,
			expected: dedent(`
                • I'll explain that Codex uses the full pulldown-cmark Markdown parser combined with targeted regex rewriting for
                  specific citation patterns like 【F:...】, which get converted into clickable links. Inline code marked by backticks
                  is only styled as dim text, not linkified, and the linking mechanics rely on actual Markdown links or those
                  citations rather than scanning inline code for paths. I'll reference key code locations that handle these steps for
                  clarity.
			`),
		},
		{
			name:     "basic paragraph - no width",
			message:  "I'll explain that Codex uses the full pulldown-cmark Markdown parser combined with targeted regex rewriting for specific citation patterns like 【F:...】, which get converted into clickable links. Inline code marked by backticks is only styled as dim text, not linkified, and the linking mechanics rely on actual Markdown links or those citations rather than scanning inline code for paths. I'll reference key code locations that handle these steps for clarity.",
			tuiWidth: 0,
			expected: "• I'll explain that Codex uses the full pulldown-cmark Markdown parser combined with targeted regex rewriting for specific citation patterns like 【F:...】, which get converted into clickable links. Inline code marked by backticks is only styled as dim text, not linkified, and the linking mechanics rely on actual Markdown links or those citations rather than scanning inline code for paths. I'll reference key code locations that handle these steps for clarity.",
		},
		{
			name:     "basic bullets",
			message:  "- Codex streams every assistant delta through MarkdownStreamCollector, which flushes complete lines only after a newline and renders them by calling append_markdown.\n- append_markdown delegates to render_markdown_text_with_citations, which runs the text through pulldown_cmark::Parser with the usual CommonMark options, so the UI is backed by a full Markdown parser rather than ad‑hoc regexes.",
			tuiWidth: 118,
			expected: dedent(`
                • - Codex streams every assistant delta through MarkdownStreamCollector, which flushes complete lines only after a
                    newline and renders them by calling append_markdown.
                  - append_markdown delegates to render_markdown_text_with_citations, which runs the text through
                    pulldown_cmark::Parser with the usual CommonMark options, so the UI is backed by a full Markdown parser rather
                    than ad‑hoc regexes.
			`),
		},
		{
			name:     "basic backticks",
			message:  "The file is located in `path/to/file.go`",
			tuiWidth: 0,
			expected: "• The file is located in " + ansiWrap("path/to/file.go", pal, colorAccent, false, false),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Convert only the bullet of the message to be ANSI escaped (so the test case looks nicer, without stuff like \x1b[38;5;153m in there).
			// Other things that need escaping will need to include it in the expected test case.
			expected := strings.Replace(tc.expected, "•", ansiWrap("•", pal, colorAccent, false, false), 1)

			event := agent.Event{
				Type: agent.EventTypeAssistantText,
				TextContent: llmstream.TextContent{
					Content: tc.message,
				},
			}
			out := formatter.FormatEvent(event, tc.tuiWidth)
			if !assert.Equal(t, strings.TrimSpace(expected), strings.TrimSpace(out)) {
				fmt.Println("EXPECTED:")
				fmt.Println(strings.TrimSpace(expected))
				fmt.Println("ACTUAL:")
				fmt.Println(strings.TrimSpace(out))
			}
		})
	}
}

func TestAssistantTextSanitizesControlCharacters(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)

	content := "\tHello\x03world\x1b[31m!\t"
	event := agent.Event{
		Type: agent.EventTypeAssistantText,
		TextContent: llmstream.TextContent{
			Content: content,
		},
	}

	out := formatter.FormatEvent(event, 60)
	require.NotEmpty(t, out)

	stripped := stripANSI(out)
	assert.Contains(t, stripped, "\\x03")
	assert.Contains(t, stripped, "\\x1B")
	assert.NotContains(t, stripped, "\t")
	assert.True(t, strings.Contains(stripped, "    Hello") || strings.Contains(stripped, "    world"),
		"tabs should expand to visible spaces in output: %q", stripped)
}

func TestAgentReasoningTableDriven(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)
	pal := newPalette(cfg)

	testCases := []struct {
		name     string
		message  string
		tuiWidth int
		expected string
	}{
		{
			name:     "basic reasoning",
			message:  "**Answering a simple question**\n\nI'm looking for the capital of France. It's Paris.",
			tuiWidth: 60,
			expected: "• " + ansiWrap("Answering a simple question", pal, colorNone, true, false),
		},
		{
			name:     "summary only reasoning",
			message:  "**Answering a simple question**",
			tuiWidth: 60,
			expected: "• " + ansiWrap("Answering a simple question", pal, colorNone, true, false),
		},
		{
			name:     "reasoning without the title",
			message:  "I'm looking for the capital of France. It's Paris.",
			tuiWidth: 60,
			expected: "• " + ansiWrap("I'm looking for the capital of France. It's Paris.", pal, colorNone, true, false),
		},
		{
			name:     "reasoning without the title - long boy",
			message:  "I'm looking for the capital of France. It's Paris. Paris is a super nice city, it has the Eiffel Tower, and baguettes. Baguettes!",
			tuiWidth: 60,
			expected: "• " + ansiWrap("I'm looking for the capital of France. It's Paris. Paris", pal, colorNone, true, false) + "\n  " +
				ansiWrap("is a super nice city, it has the Eiffel Tower, and", pal, colorNone, true, false) + "\n  " +
				ansiWrap("baguettes. Baguettes!", pal, colorNone, true, false),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Convert only the bullet of the message to be ANSI escaped (so the test case looks nicer, without stuff like \x1b[38;5;153m in there).
			// Other things that need escaping will need to include it in the expected test case.
			expected := strings.Replace(tc.expected, "•", ansiWrap("•", pal, colorAccent, false, false), 1)

			event := agent.Event{
				Type: agent.EventTypeAssistantReasoning,
				ReasoningContent: llmstream.ReasoningContent{
					Content: tc.message,
				},
			}
			out := formatter.FormatEvent(event, tc.tuiWidth)
			if !assert.Equal(t, strings.TrimSpace(expected), strings.TrimSpace(out)) {
				fmt.Println("EXPECTED:")
				fmt.Println(strings.TrimSpace(expected))
				fmt.Println("ACTUAL:")
				fmt.Println(strings.TrimSpace(out))
			}
		})
	}
}

func TestLsToolCallUsesPresenter(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)
	pal := newPalette(cfg)
	presenter := staticPresenter{
		call:     presentedReplaceSummary("List", "codeai"),
		complete: presentedReplaceSummary("List", "codeai"),
	}

	call := llmstream.ToolCall{
		Name:  "ls",
		Input: `{"path":"ignored/by/presenter"}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     testToolWithPresenter("ls", presenter),
		ToolCall: &call,
	}

	out := formatter.FormatEvent(event, 60)
	require.NotEmpty(t, out)

	expected := "• " + ansiWrap("List", pal, colorColorful, false, true) + " codeai"
	expected = strings.Replace(expected, "•", ansiWrap("•", pal, colorAccent, false, false), 1)
	assert.Equal(t, strings.TrimSpace(expected), strings.TrimSpace(out))
}

func TestGenericToolCallFallsBackToGenericFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)

	call := llmstream.ToolCall{
		Name:  "some_tool",
		Input: `{"path":"codeai"}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     testTool("some_tool"),
		ToolCall: &call,
	}

	out := formatter.FormatEvent(event, 120)
	require.NotEmpty(t, out)
	assert.Equal(t, `• Tool some_tool {"path":"codeai"}`, stripANSI(out))
}

func TestToolCallShellFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(245, 245, 245),
	}
	pal := newPalette(cfg)

	call := llmstream.ToolCall{
		Name:  "shell",
		Input: `{"command":["go","test","."]}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     testTool("shell"),
		ToolCall: &call,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 120)
	require.NotEmpty(t, out)

	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorAccent, false, false)+" "), "bullet should use accent palette")
	assert.Contains(t, out, ansiWrap("Tool", pal, colorColorful, false, true), "verb should be bold and colorful")
	assert.Equal(t, `• Tool shell {"command":["go","test","."]}`, stripANSI(out))
}

func TestToolCallSkillShellFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(245, 245, 245),
	}
	pal := newPalette(cfg)

	call := llmstream.ToolCall{
		Name:  "skill_shell",
		Input: `{"command":["go","test","."],"skill":"spec-md","timeout_ms":120000}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     testTool("skill_shell"),
		ToolCall: &call,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 160)
	require.NotEmpty(t, out)

	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorAccent, false, false)+" "))
	assert.Equal(t, `• Tool skill_shell {"command":["go","test","."],"skill":"spec-md","timeout_ms":120000}`, stripANSI(out))
}

func TestToolCompleteOutputSummarization(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)

	lines := []string{
		"ok  	axi/codeai/agentformatter    0.002s",
		"?   	axi/codeai/agentformatter/cache    [no test files]",
		"ok  	axi/codeai/agentformatter/extra    0.004s",
		"ok  	axi/codeai/agentformatter/more    0.006s",
		"ok  	axi/codeai/agentformatter/andmore    0.008s",
		"ok  	axi/codeai/agentformatter/overflow    0.010s",
		"ok  	axi/codeai/agentformatter/thelast    0.012s",
	}
	content := "Command: go test .\nProcess State: exit status 0\nTimeout: false\nDuration: 20ms\nOutput:\n" + strings.Join(lines, "\n")
	payload := map[string]any{
		"success": true,
		"content": content,
	}
	b, err := json.Marshal(payload)
	require.NoError(t, err)

	result := llmstream.ToolResult{
		Result:  string(b),
		IsError: false,
	}
	call := llmstream.ToolCall{
		Name:  "shell",
		Input: `{"command":["go","test","."]}`,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testTool("shell"),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 90)
	stripped := stripANSI(out)
	require.NotEmpty(t, stripped)

	linesOut := strings.Split(stripped, "\n")
	require.GreaterOrEqual(t, len(linesOut), 3)
	assert.Equal(t, `• Tool shell {"command":["go","test","."]}`, linesOut[0])
	assert.Equal(t, "  └ "+termformat.Sanitize(lines[0], 4), linesOut[1])
	assert.Contains(t, linesOut, "    … +2 lines")

	assert.Contains(t, out, ansiWrap("•", pal, colorGreen, false, false))
	assert.Contains(t, out, ansiWrap("Tool", pal, colorColorful, false, true))
}

func TestToolCompleteSkillShellOutputSummarization(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)

	lines := []string{
		"ok  	axi/codeai/agentformatter    0.002s",
		"?   	axi/codeai/agentformatter/cache    [no test files]",
		"ok  	axi/codeai/agentformatter/extra    0.004s",
		"ok  	axi/codeai/agentformatter/more    0.006s",
		"ok  	axi/codeai/agentformatter/andmore    0.008s",
		"ok  	axi/codeai/agentformatter/overflow    0.010s",
		"ok  	axi/codeai/agentformatter/thelast    0.012s",
	}
	content := "Command: go test .\nProcess State: exit status 0\nTimeout: false\nDuration: 20ms\nOutput:\n" + strings.Join(lines, "\n")
	payload := map[string]any{
		"success": true,
		"content": content,
	}
	b, err := json.Marshal(payload)
	require.NoError(t, err)

	result := llmstream.ToolResult{
		Result:  string(b),
		IsError: false,
	}
	call := llmstream.ToolCall{
		Name:  "skill_shell",
		Input: `{"command":["go","test","."],"skill":"spec-md"}`,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testTool("skill_shell"),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 160)
	stripped := stripANSI(out)
	require.NotEmpty(t, stripped)

	linesOut := strings.Split(stripped, "\n")
	require.GreaterOrEqual(t, len(linesOut), 3)
	assert.Equal(t, `• Tool skill_shell {"command":["go","test","."],"skill":"spec-md"}`, linesOut[0])
	assert.Equal(t, "  └ "+termformat.Sanitize(lines[0], 4), linesOut[1])
	assert.Contains(t, linesOut, "    … +2 lines")

	assert.Contains(t, out, ansiWrap("•", pal, colorGreen, false, false))
	assert.Contains(t, out, ansiWrap("Tool", pal, colorColorful, false, true))
}

func TestPresentedToolCompleteSuccessShowsOutputBody(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	formatter := NewTUIFormatter(cfg)
	presenter := staticPresenter{
		call: presentedReplaceSummary("Running", "go test ."),
		complete: presentedReplaceSummaryWithOutput("Ran", "go test .", llmstream.Output{
			Lines:            []string{"first output line wraps around cleanly", "second output line"},
			OmittedLineCount: 2,
		}),
	}

	call := llmstream.ToolCall{
		Name:  "shell",
		Input: `{"command":["go","test","."]}`,
	}
	result := llmstream.ToolResult{
		Result:  `{"success":true,"content":"ignored because presenter body owns display"}`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testToolWithPresenter("shell", presenter),
		ToolCall:   &call,
		ToolResult: &result,
	}

	t.Run("tui", func(t *testing.T) {
		out := formatter.FormatEvent(event, 34)
		require.NotEmpty(t, out)
		assert.Equal(t, []string{
			"• Ran go test .",
			"  └ first output line wraps around",
			"    cleanly",
			"    second output line",
			"    … +2 lines",
		}, strings.Split(stripANSI(out), "\n"))
		assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorGreen, false, false)+" "))
		assert.Contains(t, out, ansiWrap("Ran", pal, colorColorful, false, true))
	})

	t.Run("cli", func(t *testing.T) {
		out := formatter.FormatEvent(event, MinTerminalWidth)
		require.NotEmpty(t, out)
		assert.Equal(t, []string{
			"• Ran go test .",
			"  └ first output line wraps",
			"    around cleanly",
			"    second output line",
			"    … +2 lines",
		}, strings.Split(stripANSI(out), "\n"))
	})
}

func TestPresentedToolCompleteOutputBodyWrapsIndentedLinesWithoutBlankLine(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)
	presenter := staticPresenter{
		call: presentedReplaceSummary("Running", "sh"),
		complete: presentedReplaceSummaryWithOutput("Ran", "sh", llmstream.Output{
			Lines: []string{"    abcdefghijklmnopqrstuvwxyz"},
		}),
	}

	call := llmstream.ToolCall{
		Name:  "shell",
		Input: `{"command":["sh"]}`,
	}
	result := llmstream.ToolResult{
		Result:  `{"success":true}`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testToolWithPresenter("shell", presenter),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := formatter.FormatEvent(event, 32)
	require.NotEmpty(t, out)
	assert.Equal(t, []string{
		"• Ran sh",
		"  └     abcdefghijklmnopqrstuvwx",
		"        yz",
	}, strings.Split(stripANSI(out), "\n"))
}

func TestPresentedToolCompleteOutputBodySanitizesTabs(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)

	rawLine := "ok\tpkg\t1s"
	presenter := staticPresenter{
		call: presentedReplaceSummary("Running", "go test ./..."),
		complete: presentedReplaceSummaryWithOutput("Ran", "go test ./...", llmstream.Output{
			Lines: []string{rawLine},
		}),
	}

	call := llmstream.ToolCall{
		Name:  "shell",
		Input: `{"command":["go","test","./..."]}`,
	}
	result := llmstream.ToolResult{
		Result:  `{"success":true}`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testToolWithPresenter("shell", presenter),
		ToolCall:   &call,
		ToolResult: &result,
	}

	expected := []string{
		"• Ran go test ./...",
		"  └ " + termformat.Sanitize(rawLine, 4),
	}

	t.Run("tui", func(t *testing.T) {
		out := formatter.FormatEvent(event, 120)
		require.NotEmpty(t, out)
		assert.Equal(t, expected, strings.Split(stripANSI(out), "\n"))
		assert.NotContains(t, stripANSI(out), "\t")
	})

	t.Run("cli", func(t *testing.T) {
		out := formatter.FormatEvent(event, MinTerminalWidth)
		require.NotEmpty(t, out)
		assert.Equal(t, expected, strings.Split(stripANSI(out), "\n"))
		assert.NotContains(t, stripANSI(out), "\t")
	})
}

func TestPresentedToolCompleteOutputBodyWithTabsRespectsTUIWidth(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)

	rawLine := "ok\tgithub.com/codalotl/codalotl/internal/agentformatter\t(cached)"
	presenter := staticPresenter{
		call: presentedReplaceSummary("Running", "go test ./..."),
		complete: presentedReplaceSummaryWithOutput("Ran", "go test ./...", llmstream.Output{
			Lines: []string{rawLine},
		}),
	}

	call := llmstream.ToolCall{
		Name:  "shell",
		Input: `{"command":["go","test","./..."]}`,
	}
	result := llmstream.ToolResult{
		Result:  `{"success":true}`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testToolWithPresenter("shell", presenter),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := formatter.FormatEvent(event, 44)
	require.NotEmpty(t, out)

	stripped := stripANSI(out)
	assert.NotContains(t, stripped, "\t")
	for _, line := range strings.Split(out, "\n") {
		assert.LessOrEqual(t, termformat.TextWidthWithANSICodes(line), 44)
	}
	assert.Contains(t, stripped, "• Ran go test ./...")
	assert.Contains(t, stripped, "  └ ok")
	assert.Contains(t, stripped, "(cached)")
}

func TestPresentedToolCompleteErrorStillUsesSharedErrorFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	formatter := NewTUIFormatter(cfg)
	presenter := staticPresenter{
		call: presentedReplaceSummary("Running", "go test ."),
		complete: presentedReplaceSummaryWithOutput("Ran", "go test .", llmstream.Output{
			Lines: []string{"presenter body should not override tool errors"},
		}),
	}

	call := llmstream.ToolCall{
		Name:  "shell",
		Input: `{"command":["go","test","."]}`,
	}
	result := llmstream.ToolResult{
		Result:  "exec: go: not found",
		IsError: true,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testToolWithPresenter("shell", presenter),
		ToolCall:   &call,
		ToolResult: &result,
	}

	expected := []string{
		"• Ran go test .",
		"  └ Error: exec: go: not found",
	}

	t.Run("tui", func(t *testing.T) {
		out := formatter.FormatEvent(event, 80)
		require.NotEmpty(t, out)
		assert.Equal(t, expected, strings.Split(stripANSI(out), "\n"))
		assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorRed, false, false)+" "))
		assert.NotContains(t, stripANSI(out), "presenter body should not override tool errors")
	})

	t.Run("cli", func(t *testing.T) {
		out := formatter.FormatEvent(event, MinTerminalWidth)
		require.NotEmpty(t, out)
		assert.Equal(t, expected, strings.Split(stripANSI(out), "\n"))
		assert.NotContains(t, stripANSI(out), "presenter body should not override tool errors")
	})
}

func TestPresentedUpdatePlanMatchesExpectedFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)
	tool := newUpdatePlanTool(t)

	explanation := "Need to align CodeUnit authorizer with updated SPEC behavior for read-only restrictions and adjust tests accordingly."
	stepDone := "Inspect SPEC changes and current CodeUnit authorizer implementation"
	stepDoing := "Update codeunit authorizer logic to apply read restrictions only to read_file tool and keep write restrictions for all tools"
	stepTodo := "Revise tests to cover new behavior and run go test for package"
	input := `{
  "explanation": "` + explanation + `",
  "plan": [
    {"step":"` + stepDone + `","status":"completed"},
    {"step":"` + stepDoing + `","status":"in_progress"},
    {"step":"` + stepTodo + `","status":"pending"}
  ]
}`

	call := llmstream.ToolCall{
		Name:  "update_plan",
		Input: input,
	}

	callEvent := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     tool,
		ToolCall: &call,
	}
	completeEvent := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       tool,
		ToolCall:   &call,
		ToolResult: &llmstream.ToolResult{Result: `{"success":true}`},
	}

	expected := []string{
		"• Update Plan",
		"  └ " + explanation,
		"    ✔ " + stepDone,
		"    □ " + stepDoing,
		"    □ " + stepTodo,
	}

	t.Run("call tui", func(t *testing.T) {
		assert.Equal(t, expected, strings.Split(stripANSI(formatter.FormatEvent(callEvent, 400)), "\n"))
	})

	t.Run("call cli", func(t *testing.T) {
		assert.Equal(t, []string{
			"• Update Plan",
			"  └ Need to align CodeUnit",
			"    authorizer with updated",
			"    SPEC behavior for",
			"    read-only restrictions and",
			"    adjust tests accordingly.",
			"    ✔ Inspect SPEC changes and",
			"    current CodeUnit",
			"    authorizer implementation",
			"    □ Update codeunit",
			"    authorizer logic to apply",
			"    read restrictions only to",
			"    read_file tool and keep",
			"    write restrictions for all",
			"    tools",
			"    □ Revise tests to cover",
			"    new behavior and run go",
			"    test for package",
		}, strings.Split(stripANSI(formatter.FormatEvent(callEvent, MinTerminalWidth)), "\n"))
	})

	t.Run("complete tui", func(t *testing.T) {
		assert.Equal(t, expected, strings.Split(stripANSI(formatter.FormatEvent(completeEvent, 400)), "\n"))
	})

	t.Run("complete cli", func(t *testing.T) {
		assert.Equal(t, []string{
			"• Update Plan",
			"  └ Need to align CodeUnit",
			"    authorizer with updated",
			"    SPEC behavior for",
			"    read-only restrictions and",
			"    adjust tests accordingly.",
			"    ✔ Inspect SPEC changes and",
			"    current CodeUnit",
			"    authorizer implementation",
			"    □ Update codeunit",
			"    authorizer logic to apply",
			"    read restrictions only to",
			"    read_file tool and keep",
			"    write restrictions for all",
			"    tools",
			"    □ Revise tests to cover",
			"    new behavior and run go",
			"    test for package",
		}, strings.Split(stripANSI(formatter.FormatEvent(completeEvent, MinTerminalWidth)), "\n"))
	})
}

func TestPresentedToolCompleteDiffBodyMatchesApplyPatchStyle(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	formatter := NewTUIFormatter(cfg)

	patch := `*** Begin Patch
*** Update File: foo/bar.go
*** Move to: foo/baz.go
@@
- old line
+ replacement line that wraps across multiple words in the presenter diff renderer
@@
+ final line
*** End Patch
`
	presenter := staticPresenter{
		call: presentedReplaceSummary("Apply Patch", ""),
		complete: presentedReplaceSummaryWithBody(
			"Apply Patch",
			"",
			llmstream.Diff{
				Edits: []llmstream.DiffEdit{
					{
						Kind:    llmstream.DiffEditKindRename,
						OldPath: "foo/bar.go",
						NewPath: "foo/baz.go",
						Lines: []llmstream.DiffLine{
							{Kind: llmstream.DiffLineKindDelete, Text: "old line"},
							{Kind: llmstream.DiffLineKindAdd, Text: "replacement line that wraps across multiple words in the presenter diff renderer"},
							{Kind: llmstream.DiffLineKindOmitted},
							{Kind: llmstream.DiffLineKindAdd, Text: "final line"},
						},
					},
				},
			},
		),
	}

	call := llmstream.ToolCall{
		Name:  "apply_patch",
		Input: patch,
	}
	result := llmstream.ToolResult{
		Result:  `{"success":true}`,
		IsError: false,
	}

	presentedEvent := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testToolWithPresenter("apply_patch", presenter),
		ToolCall:   &call,
		ToolResult: &result,
	}
	explicitEvent := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       newApplyPatchTool(t),
		ToolCall:   &call,
		ToolResult: &result,
	}

	assertBodyMatches := func(t *testing.T, width int) {
		t.Helper()

		presentedLines := strings.Split(stripANSI(formatter.FormatEvent(presentedEvent, width)), "\n")
		explicitLines := strings.Split(stripANSI(formatter.FormatEvent(explicitEvent, width)), "\n")
		require.NotEmpty(t, presentedLines)
		require.NotEmpty(t, explicitLines)
		assert.Equal(t, explicitLines, presentedLines)
	}

	t.Run("tui", func(t *testing.T) {
		out := formatter.FormatEvent(presentedEvent, 58)
		assertBodyMatches(t, 58)
		assert.Contains(t, out, ansiWrap("-", pal, colorRed, false, false))
		assert.Contains(t, out, ansiWrap("+", pal, colorGreen, false, false))
		assert.Contains(t, out, ansiWrap("⋮", pal, colorAccent, false, false))
	})

	t.Run("minimum tui width", func(t *testing.T) {
		out := formatter.FormatEvent(presentedEvent, MinTerminalWidth)
		explicitOut := formatter.FormatEvent(explicitEvent, MinTerminalWidth)
		lines := strings.Split(out, "\n")
		require.NotEmpty(t, lines)
		require.Equal(t, stripANSI(explicitOut), stripANSI(out))
	})
}

func TestPresentedDiffBodyRejectsExplicitSummary(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)

	event := agent.Event{
		Type: agent.EventTypeToolComplete,
		Tool: testToolWithPresenter("apply_patch", staticPresenter{
			complete: llmstream.Presentation{
				Behavior: llmstream.CompletionBehaviorReplace,
				Summary: llmstream.Line{
					Segments: []llmstream.Segment{
						{Text: "Apply Patch", Role: llmstream.RoleAction},
					},
				},
				Body: llmstream.Diff{
					Edits: []llmstream.DiffEdit{{
						Kind:    llmstream.DiffEditKindEdit,
						OldPath: "foo/bar.go",
					}},
				},
			},
		}),
		ToolCall:   &llmstream.ToolCall{Name: "apply_patch"},
		ToolResult: &llmstream.ToolResult{Name: "apply_patch", Result: `{"success":true}`},
	}

	out := formatter.FormatEvent(event, 120)
	require.NotEmpty(t, out)
	assert.Equal(t, "• Error presenter diff bodies must leave Summary.Segments nil", stripANSI(out))
}

func TestPresentedToolCompleteSemanticBodyErrorStillOverridesBody(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)

	presenter := staticPresenter{
		call: presentedReplaceSummary("Update Plan", ""),
		complete: presentedReplaceSummaryWithBody(
			"Update Plan",
			"",
			llmstream.Paragraph{
				Lines: []llmstream.Line{{
					Segments: []llmstream.Segment{{Text: "This body should be suppressed on error.", Role: llmstream.RoleAccent}},
				}},
			},
		),
	}

	call := llmstream.ToolCall{
		Name:  "update_plan",
		Input: `{"explanation":"ignored by presenter"}`,
	}
	result := llmstream.ToolResult{
		Result:  "update failed",
		IsError: true,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testToolWithPresenter("update_plan", presenter),
		ToolCall:   &call,
		ToolResult: &result,
	}

	expected := []string{
		"• Update Plan",
		"  └ Error: update failed",
	}

	t.Run("tui", func(t *testing.T) {
		out := formatter.FormatEvent(event, 100)
		assert.Equal(t, expected, strings.Split(stripANSI(out), "\n"))
		assert.NotContains(t, stripANSI(out), "This body should be suppressed on error.")
		assert.NotContains(t, stripANSI(out), "Do the thing")
	})

	t.Run("cli", func(t *testing.T) {
		out := formatter.FormatEvent(event, MinTerminalWidth)
		assert.Equal(t, expected, strings.Split(stripANSI(out), "\n"))
		assert.NotContains(t, stripANSI(out), "This body should be suppressed on error.")
		assert.NotContains(t, stripANSI(out), "Do the thing")
	})
}

func TestToolCallReadFileVerbColor(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(240, 240, 240),
	}
	pal := newPalette(cfg)
	presenter := staticPresenter{
		call:     presentedReplaceSummary("Read", "codeai/tools/shell.go"),
		complete: presentedReplaceSummary("Read", "codeai/tools/shell.go"),
	}

	call := llmstream.ToolCall{
		Name:  "read_file",
		Input: `{"path":"ignored/by/presenter.go"}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     testToolWithPresenter("read_file", presenter),
		ToolCall: &call,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 72)
	require.NotEmpty(t, out)

	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorAccent, false, false)+" "), "bullet should be accent for in-progress tool call")
	colorfulVerb := ansiWrap("Read", pal, colorColorful, false, true)
	assert.Contains(t, out, colorfulVerb, "verb should be colorful and bold")
	if accent := pal.style(runeStyle{color: colorAccent}).OpeningControlCodes(); accent != "" {
		assert.NotContains(t, out, accent+"Read", "verb should not use accent color")
	}
	assert.Contains(t, stripANSI(out), "Read codeai/tools/shell.go")
}

func TestReadFileCompleteSuccessNoOutput(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	presenter := staticPresenter{
		call:     presentedReplaceSummary("Read", "codeai/tools/shell.go"),
		complete: presentedReplaceSummary("Read", "codeai/tools/shell.go"),
	}

	call := llmstream.ToolCall{
		Name:  "read_file",
		Input: `{"path":"ignored/by/presenter.go"}`,
	}
	result := llmstream.ToolResult{
		Result: `<file name="codeai/tools/shell.go" line-count="2" byte-count="20" truncated="false">
line 1
line 2
</file>
`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testToolWithPresenter("read_file", presenter),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 100)
	require.NotEmpty(t, out)

	stripped := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{"• Read codeai/tools/shell.go"}, stripped)
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorGreen, false, false)+" "))
	assert.NotContains(t, out, "└")
}

func TestReadFileCompleteErrorShowsMessage(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	presenter := staticPresenter{
		call:     presentedReplaceSummary("Read", "missing.txt"),
		complete: presentedReplaceSummary("Read", "missing.txt"),
	}

	call := llmstream.ToolCall{
		Name:  "read_file",
		Input: `{"path":"ignored/by/presenter.txt"}`,
	}
	result := llmstream.ToolResult{
		Result:  "path does not exist",
		IsError: true,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testToolWithPresenter("read_file", presenter),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 100)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Read missing.txt",
		"  └ Error: path does not exist",
	}, lines)
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorRed, false, false)+" "))
}

func TestAppendPresenterFormatsToolCall(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	presenter := staticPresenter{
		call: llmstream.Presentation{
			Behavior: llmstream.CompletionBehaviorAppend,
			Summary: llmstream.Line{
				Segments: []llmstream.Segment{
					{Text: "Presented", Role: llmstream.RoleAction},
					{Text: " by presenter", Role: llmstream.RoleNormal},
				},
			},
		},
		complete: llmstream.Presentation{
			Behavior: llmstream.CompletionBehaviorAppend,
			Summary: llmstream.Line{
				Segments: []llmstream.Segment{
					{Text: "Presented", Role: llmstream.RoleAction},
					{Text: " by presenter", Role: llmstream.RoleNormal},
				},
			},
		},
	}

	call := llmstream.ToolCall{
		Name:  "shell",
		Input: `{"command":["go","test","."]}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     testToolWithPresenter("shell", presenter),
		ToolCall: &call,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 72)
	require.NotEmpty(t, out)
	assert.Equal(t, "• Presented by presenter", stripANSI(out))
}

func TestPresentedToolSummaryJoinWithSpace(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)

	testCases := []struct {
		name     string
		line     llmstream.Line
		expected string
	}{
		{
			name: "false preserves adjacent text",
			line: llmstream.Line{
				Segments: []llmstream.Segment{
					{Text: "foo", Role: llmstream.RoleAction},
					{Text: "(bar)", Role: llmstream.RoleNormal},
				},
			},
			expected: "• foo(bar)",
		},
		{
			name: "true inserts single space",
			line: llmstream.Line{
				JoinWithSpace: true,
				Segments: []llmstream.Segment{
					{Text: "foo", Role: llmstream.RoleAction},
					{Text: "bar", Role: llmstream.RoleNormal},
				},
			},
			expected: "• foo bar",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			presenter := staticPresenter{
				call:     presentedReplaceLine(tc.line),
				complete: presentedReplaceLine(tc.line),
			}
			call := llmstream.ToolCall{
				Name:  "read_file",
				Input: `{"path":"ignored/by/presenter.go"}`,
			}

			callEvent := agent.Event{
				Type:     agent.EventTypeToolCall,
				Tool:     testToolWithPresenter("read_file", presenter),
				ToolCall: &call,
			}
			completeEvent := agent.Event{
				Type:     agent.EventTypeToolComplete,
				Tool:     testToolWithPresenter("read_file", presenter),
				ToolCall: &call,
				ToolResult: &llmstream.ToolResult{
					Result:  `{"success":true}`,
					IsError: false,
				},
			}

			assert.Equal(t, tc.expected, stripANSI(formatter.FormatEvent(callEvent, 120)))
			assert.Equal(t, tc.expected, stripANSI(formatter.FormatEvent(completeEvent, 120)))
			assert.Equal(t, tc.expected, stripANSI(formatter.FormatEvent(callEvent, MinTerminalWidth)))
			assert.Equal(t, tc.expected, stripANSI(formatter.FormatEvent(completeEvent, MinTerminalWidth)))
		})
	}
}

func TestPresentedToolTUIWidthLimit(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)

	makeEvent := func(presentation llmstream.Presentation) agent.Event {
		call := llmstream.ToolCall{
			Name:  "read_file",
			Input: `{"path":"ignored/by/presenter.go"}`,
		}
		result := llmstream.ToolResult{
			Result:  `{"success":true}`,
			IsError: false,
		}
		return agent.Event{
			Type:       agent.EventTypeToolComplete,
			Tool:       testToolWithPresenter("read_file", staticPresenter{complete: presentation}),
			ToolCall:   &call,
			ToolResult: &result,
		}
	}

	assertWidthLimit := func(t *testing.T, width int, out string) {
		t.Helper()
		for _, line := range strings.Split(out, "\n") {
			assert.LessOrEqual(t, termformat.TextWidthWithANSICodes(line), width)
		}
	}

	t.Run("summary wraps", func(t *testing.T) {
		event := makeEvent(presentedReplaceSummary("Read", "some/really/long/path/that/needs/to/wrap/in/the/tui/output.go"))
		out := formatter.FormatEvent(event, 36)
		require.NotEmpty(t, out)
		assertWidthLimit(t, 36, out)
	})

	t.Run("summary wraps at minimum tui width", func(t *testing.T) {
		event := makeEvent(presentedReplaceSummary("Read", "some/really/long/path/that/needs/to/wrap/in/the/tui/output.go"))
		out := formatter.FormatEvent(event, MinTerminalWidth)
		require.NotEmpty(t, out)
		assertWidthLimit(t, MinTerminalWidth, out)
	})

	t.Run("zero width still uses cli fallback", func(t *testing.T) {
		event := makeEvent(presentedReplaceSummary("Read", "some/really/long/path/that/needs/to/wrap/in/the/tui/output.go"))
		out := formatter.FormatEvent(event, 0)
		require.NotEmpty(t, out)
		assert.Greater(t, termformat.TextWidthWithANSICodes(out), MinTerminalWidth)
		assert.NotContains(t, out, "\n")
	})

	t.Run("paragraph body wraps", func(t *testing.T) {
		event := makeEvent(presentedReplaceSummaryWithBody(
			"Update Plan",
			"",
			llmstream.Paragraph{
				Lines: []llmstream.Line{{
					Segments: []llmstream.Segment{{
						Text: "This presenter paragraph line is intentionally long enough to require wrapping inside a narrow TUI.",
						Role: llmstream.RoleAccent,
					}},
				}},
			},
		))
		out := formatter.FormatEvent(event, 36)
		require.NotEmpty(t, out)
		assertWidthLimit(t, 36, out)
	})

	t.Run("paragraph body wraps at minimum tui width", func(t *testing.T) {
		event := makeEvent(presentedReplaceSummaryWithBody(
			"Update Plan",
			"",
			llmstream.Paragraph{
				Lines: []llmstream.Line{{
					Segments: []llmstream.Segment{{
						Text: "This presenter paragraph line is intentionally long enough to require wrapping inside a narrow TUI.",
						Role: llmstream.RoleAccent,
					}},
				}},
			},
		))
		out := formatter.FormatEvent(event, MinTerminalWidth)
		require.NotEmpty(t, out)
		assertWidthLimit(t, MinTerminalWidth, out)
	})

	t.Run("checklist body wraps", func(t *testing.T) {
		event := makeEvent(presentedReplaceSummaryWithBody(
			"Update Plan",
			"",
			llmstream.Checklist{
				Items: []llmstream.ChecklistItem{{
					Status: llmstream.ChecklistStatusInProgress,
					Line: llmstream.Line{
						Segments: []llmstream.Segment{{
							Text: "Check that an in-progress presenter checklist item still respects the requested width.",
							Role: llmstream.RoleAction,
						}},
					},
				}},
			},
		))
		out := formatter.FormatEvent(event, 36)
		require.NotEmpty(t, out)
		assertWidthLimit(t, 36, out)
	})

	t.Run("checklist overview renders before items", func(t *testing.T) {
		event := makeEvent(presentedReplaceSummaryWithBody(
			"Update Plan",
			"",
			llmstream.Checklist{
				Overview: llmstream.Line{
					Segments: []llmstream.Segment{{
						Text: "Need to align tool rendering with presenter output.",
						Role: llmstream.RoleAccent,
					}},
				},
				Items: []llmstream.ChecklistItem{{
					Status: llmstream.ChecklistStatusInProgress,
					Line: llmstream.Line{
						Segments: []llmstream.Segment{{
							Text: "Keep the visible plan output unchanged.",
							Role: llmstream.RoleAction,
						}},
					},
				}},
			},
		))

		out := formatter.FormatEvent(event, 100)
		assert.Equal(t, []string{
			"• Update Plan",
			"  └ Need to align tool rendering with presenter output.",
			"    □ Keep the visible plan output unchanged.",
		}, strings.Split(stripANSI(out), "\n"))
	})

	t.Run("checklist body wraps at minimum tui width", func(t *testing.T) {
		event := makeEvent(presentedReplaceSummaryWithBody(
			"Update Plan",
			"",
			llmstream.Checklist{
				Items: []llmstream.ChecklistItem{{
					Status: llmstream.ChecklistStatusInProgress,
					Line: llmstream.Line{
						Segments: []llmstream.Segment{{
							Text: "Check that an in-progress presenter checklist item still respects the requested width.",
							Role: llmstream.RoleAction,
						}},
					},
				}},
			},
		))
		out := formatter.FormatEvent(event, MinTerminalWidth)
		require.NotEmpty(t, out)
		assertWidthLimit(t, MinTerminalWidth, out)
	})
}

func TestToolCompleteSillyAgentOutsidePackageReadFileTUI(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)

	call := llmstream.ToolCall{
		Name:  "read_file",
		Input: `{"path":"some/file.go"}`,
	}
	result := llmstream.ToolResult{
		Result:    "denied",
		IsError:   true,
		SourceErr: authdomain.ErrCodeUnitPathOutside,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testTool("read_file"),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 100)
	require.NotEmpty(t, out)

	require.Equal(t, "• Silly LLM tried read_file on some/file.go outside of package.", stripANSI(out))
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorRed, false, false)+" "))
	assert.Contains(t, out, ansiWrap("Silly LLM tried read_file on some/file.go outside of package.", pal, colorAccent, false, false))
	assert.NotContains(t, stripANSI(out), "└")
}

func TestToolCompleteSillyLLMOutsidePackageReadFileCLI(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}

	call := llmstream.ToolCall{
		Name:  "read_file",
		Input: `{"path":"some/file.go"}`,
	}
	result := llmstream.ToolResult{
		Result:    "denied",
		IsError:   true,
		SourceErr: authdomain.ErrCodeUnitPathOutside,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testTool("read_file"),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, MinTerminalWidth)
	require.NotEmpty(t, out)
	require.Equal(t, "• Silly LLM tried read_file on some/file.go outside of package.", stripANSI(out))
}

func TestToolCompleteSillyLLMOutsidePackageNoPath(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}

	call := llmstream.ToolCall{
		Name:  "apply_patch",
		Input: `*** Begin Patch` + "\n" + `*** End Patch` + "\n",
	}
	result := llmstream.ToolResult{
		Result:    "denied",
		IsError:   true,
		SourceErr: authdomain.ErrCodeUnitPathOutside,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       newApplyPatchTool(t),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 100)
	require.NotEmpty(t, out)
	require.Equal(t, "• Silly LLM tried apply_patch outside of package.", stripANSI(out))
}

func TestDiagnosticsToolCallFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "diagnostics",
		Input: `{"path":"./internal/agentformatter"}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     extToolWithPresenter(t, exttools.NewDiagnosticsTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))),
		ToolCall: &call,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 120)
	require.NotEmpty(t, out)
	require.Equal(t, "• Run Diagnostics ./internal/agentformatter", stripANSI(out))
	assert.Contains(t, out, ansiWrap("•", pal, colorAccent, false, false))
	assert.Contains(t, out, ansiWrap("Run Diagnostics", pal, colorColorful, false, true))
	assert.NotContains(t, out, "└")
}

func TestDiagnosticsToolCompleteSuccessNoOutput(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "diagnostics",
		Input: `{"path":"./internal/agentformatter"}`,
	}
	result := llmstream.ToolResult{
		Result:  `{"success":true,"content":"build succeeded"}`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       extToolWithPresenter(t, exttools.NewDiagnosticsTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 120)
	require.NotEmpty(t, out)
	require.Equal(t, "• Ran Diagnostics ./internal/agentformatter", stripANSI(out))
	assert.Contains(t, out, ansiWrap("•", pal, colorGreen, false, false))
	assert.NotContains(t, out, "└")
}

func TestDiagnosticsToolCompleteFailureNoOutput(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "diagnostics",
		Input: `{"path":"./internal/agentformatter"}`,
	}
	// diagnostics may return a non-error ToolResult with success=false and a content/error message;
	// per SPEC we still only show the single-line header and indicate status via bullet color.
	result := llmstream.ToolResult{
		Result:  `{"success":false,"error":"build failed"}`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       extToolWithPresenter(t, exttools.NewDiagnosticsTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 120)
	require.NotEmpty(t, out)
	require.Equal(t, "• Ran Diagnostics ./internal/agentformatter", stripANSI(out))
	assert.Contains(t, out, ansiWrap("•", pal, colorRed, false, false))
	assert.NotContains(t, stripANSI(out), "Error:")
	assert.NotContains(t, out, "└")
}

func TestDiagnosticsToolCompleteFailureFromXMLTagNoOutput(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "diagnostics",
		Input: `{"path":"./internal/agentformatter"}`,
	}
	result := llmstream.ToolResult{
		Result: `<diagnostics-status ok="false">
$ go build -o /dev/null ./internal/agentformatter
# github.com/codalotl/codalotl/internal/agentformatter
agentformatter.go:1:1: some error
</diagnostics-status>`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       extToolWithPresenter(t, exttools.NewDiagnosticsTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 120)
	require.NotEmpty(t, out)
	require.Equal(t, "• Ran Diagnostics ./internal/agentformatter", stripANSI(out))
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorRed, false, false)+" "))
	assert.NotContains(t, stripANSI(out), "Error:")
	assert.NotContains(t, out, "└")
}

func TestDiagnosticsToolCompleteSuccessFromXMLTagNoOutput(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "diagnostics",
		Input: `{"path":"./internal/agentformatter"}`,
	}
	result := llmstream.ToolResult{
		Result: `<diagnostics-status ok="true" message="build succeeded">
$ go build -o /dev/null ./internal/agentformatter
</diagnostics-status>`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       extToolWithPresenter(t, exttools.NewDiagnosticsTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 120)
	require.NotEmpty(t, out)
	require.Equal(t, "• Ran Diagnostics ./internal/agentformatter", stripANSI(out))
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorGreen, false, false)+" "))
	assert.NotContains(t, out, "└")
}

func TestDiagnosticsToolCompleteCLI(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	call := llmstream.ToolCall{
		Name:  "diagnostics",
		Input: `{"path":"./internal/agentformatter"}`,
	}
	result := llmstream.ToolResult{
		Result:  `{"success":true}`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       extToolWithPresenter(t, exttools.NewDiagnosticsTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, MinTerminalWidth)
	require.NotEmpty(t, out)
	require.Equal(t, "• Ran Diagnostics ./internal/agentformatter", stripANSI(out))
}

func TestFixLintsToolCallFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "fix_lints",
		Input: `{"path":"./internal/agentformatter"}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     extToolWithPresenter(t, exttools.NewFixLintsTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), nil)),
		ToolCall: &call,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 120)
	require.NotEmpty(t, out)
	require.Equal(t, "• Fix Lints ./internal/agentformatter", stripANSI(out))
	assert.Contains(t, out, ansiWrap("•", pal, colorAccent, false, false))
	assert.Contains(t, out, ansiWrap("Fix Lints", pal, colorColorful, false, true))
	assert.NotContains(t, out, "└")
}

func TestFixLintsToolCompleteSuccessShowsOutput(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "fix_lints",
		Input: `{"path":"./internal/agentformatter"}`,
	}
	result := llmstream.ToolResult{
		Result: `<lint-status ok="true">
<command ok="true" message="no issues found" mode="fix">
$ gofmt -l -w internal/agentformatter
</command>
<command ok="true" mode="fix">
$ codalotl docs reflow internal/agentformatter
internal/agentformatter/agentformatter.go
</command>
</lint-status>`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       extToolWithPresenter(t, exttools.NewFixLintsTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), nil)),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 200)
	require.NotEmpty(t, out)

	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Fixed Lints ./internal/agentformatter",
		"  └ $ gofmt -l -w internal/agentformatter",
		"    $ codalotl docs reflow internal/agentformatter",
		"    internal/agentformatter/agentformatter.go",
	}, lines)

	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorGreen, false, false)+" "))
	assert.Contains(t, out, ansiWrap("Fixed Lints", pal, colorColorful, false, true))
	assert.NotContains(t, stripANSI(out), "<command")
}

func TestFixLintsToolCompleteCLI(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	call := llmstream.ToolCall{
		Name:  "fix_lints",
		Input: `{"path":"./internal/agentformatter"}`,
	}
	result := llmstream.ToolResult{
		Result:  `{"success":true,"content":"$ gofmt -l -w internal/agentformatter"}`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       extToolWithPresenter(t, exttools.NewFixLintsTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), nil)),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, MinTerminalWidth)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Fixed Lints ./internal/agentformatter",
		"  └ $ gofmt -l -w internal/agentformatter",
	}, lines)
}

func TestLsPresenterCompleteSuccessNoOutput(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	presenter := staticPresenter{
		call:     presentedReplaceSummary("List", "."),
		complete: presentedReplaceSummary("List", "."),
	}

	call := llmstream.ToolCall{
		Name:  "ls",
		Input: `{"path":"ignored/by/presenter"}`,
	}
	result := llmstream.ToolResult{
		Result:  `{"success":true,"content":"- file1\n- file2"}`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testToolWithPresenter("ls", presenter),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 100)
	require.NotEmpty(t, out)
	stripped := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{"• List ."}, stripped)
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorGreen, false, false)+" "))
	assert.NotContains(t, out, "└")
}

func TestLsPresenterCompleteErrorShowsMessage(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	presenter := staticPresenter{
		call:     presentedReplaceSummary("List", "/tmp/unknown"),
		complete: presentedReplaceSummary("List", "/tmp/unknown"),
	}

	call := llmstream.ToolCall{
		Name:  "ls",
		Input: `{"path":"ignored/by/presenter"}`,
	}
	result := llmstream.ToolResult{
		Result:  "path does not exist",
		IsError: true,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testToolWithPresenter("ls", presenter),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 100)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• List /tmp/unknown",
		"  └ Error: path does not exist",
	}, lines)
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorRed, false, false)+" "))
}

func TestApplyPatchCallFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	patch := `*** Begin Patch
*** Update File: foo/bar.go
@@
- old line
+ new line
*** End Patch
`
	call := llmstream.ToolCall{
		Name:  "apply_patch",
		Input: patch,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     newApplyPatchTool(t),
		ToolCall: &call,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 80)
	require.NotEmpty(t, out)

	stripped := stripANSI(out)
	expected := []string{
		"• Edit foo/bar.go",
		"     - old line",
		"     + new line",
	}
	require.Equal(t, expected, strings.Split(stripped, "\n"))
	assert.NotContains(t, stripped, "└")
	assert.Contains(t, out, ansiWrap("-", pal, colorRed, false, false))
	assert.Contains(t, out, ansiWrap("+", pal, colorGreen, false, false))
}

func TestApplyPatchMultiEditCallFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	patch := `*** Begin Patch
*** Add File: foo/new.txt
+first line
*** Delete File: foo/old.txt
*** Update File: foo/bar.go
*** Move to: foo/baz.go
@@
-old line
+new line
*** End Patch
`
	call := llmstream.ToolCall{
		Name:  "apply_patch",
		Input: patch,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     newApplyPatchTool(t),
		ToolCall: &call,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 90)
	require.NotEmpty(t, out)
	require.Equal(t, []string{
		"• Add foo/new.txt",
		"     + first line",
		"• Delete foo/old.txt",
		"• Edit foo/bar.go → foo/baz.go",
		"     - old line",
		"     + new line",
	}, strings.Split(stripANSI(out), "\n"))
	assert.NotContains(t, stripANSI(out), "Apply Patch")
	assert.NotContains(t, stripANSI(out), "└ Delete")
}

func TestApplyPatchContextLinesAreNormalColor(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	patch := `*** Begin Patch
*** Update File: foo/bar.go
@@
 context before
- old line
+ new line
 context after
*** End Patch
`
	call := llmstream.ToolCall{
		Name:  "apply_patch",
		Input: patch,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     newApplyPatchTool(t),
		ToolCall: &call,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 80)
	require.NotEmpty(t, out)
	assert.Contains(t, out, ansiWrap("context before", pal, colorNormal, false, false))
	assert.Contains(t, out, ansiWrap("context after", pal, colorNormal, false, false))
}

func TestApplyPatchLinesSanitized(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	patch := "*** Begin Patch\n*** Update File: foo/tabs.go\n@@\n- old\tline\n+ new\x03line\n*** End Patch\n"
	call := llmstream.ToolCall{
		Name:  "apply_patch",
		Input: patch,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     newApplyPatchTool(t),
		ToolCall: &call,
	}
	ctrlC := string([]byte{0x03})
	formatter := NewTUIFormatter(cfg)

	tuiOut := formatter.FormatEvent(event, 80)
	require.NotEmpty(t, tuiOut)
	tuiLines := strings.Split(stripANSI(tuiOut), "\n")
	expected := []string{
		"• Edit foo/tabs.go",
		"     - old    line",
		"     + new\\x03line",
	}
	require.Equal(t, expected, tuiLines)
	assert.NotContains(t, tuiOut, "\t")
	assert.NotContains(t, tuiOut, ctrlC)

	cliOut := formatter.FormatEvent(event, MinTerminalWidth)
	require.NotEmpty(t, cliOut)
	cliLines := strings.Split(stripANSI(cliOut), "\n")
	require.Equal(t, expected, cliLines)
	assert.NotContains(t, cliOut, "\t")
	assert.NotContains(t, cliOut, ctrlC)
}
func TestEditToolCallUsesPresenterAndKeepsReplaceAllUX(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "edit",
		Input: `{"file_path":"foo/bar.go","old_string":"old line","new_string":"new line","replace_all":true}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     newEditTool(t),
		ToolCall: &call,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 90)
	require.NotEmpty(t, out)
	require.Equal(t, []string{
		"• Edit foo/bar.go (replace all)",
		"     - old line",
		"     + new line",
	}, strings.Split(stripANSI(out), "\n"))
	assert.Contains(t, out, ansiWrap("•", pal, colorAccent, false, false))
	assert.Contains(t, out, ansiWrap("-", pal, colorRed, false, false))
	assert.Contains(t, out, ansiWrap("+", pal, colorGreen, false, false))
}
func TestWriteToolCallUsesPresenterAndKeepsUX(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "write",
		Input: `{"path":"foo/new.txt","content":"first line\nsecond line"}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     newWriteTool(t),
		ToolCall: &call,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 90)
	require.NotEmpty(t, out)
	require.Equal(t, []string{
		"• Add foo/new.txt",
		"     + first line",
		"     + second line",
	}, strings.Split(stripANSI(out), "\n"))
	assert.Contains(t, out, ansiWrap("•", pal, colorAccent, false, false))
	assert.Contains(t, out, ansiWrap("+", pal, colorGreen, false, false))
}
func TestDeleteToolCallUsesPresenter(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	tool := newDeleteTool(t)
	require.NotNil(t, tool.Presenter())

	call := llmstream.ToolCall{
		Name:  "delete",
		Input: `{"path":"foo/old.txt"}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     tool,
		ToolCall: &call,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 90)
	require.NotEmpty(t, out)
	require.Equal(t, "• Delete foo/old.txt", stripANSI(out))
	assert.Contains(t, out, ansiWrap("•", pal, colorAccent, false, false))
	assert.Contains(t, out, ansiWrap("Delete", pal, colorColorful, false, true))
}

func TestDeleteToolCompleteSuccessUsesPresenter(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)

	call := llmstream.ToolCall{
		Name:  "delete",
		Input: `{"path":"foo/old.txt"}`,
	}
	result := llmstream.ToolResult{
		Result:  `{"success":true}`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       newDeleteTool(t),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 90)
	require.NotEmpty(t, out)
	require.Equal(t, "• Delete foo/old.txt", stripANSI(out))
	assert.Contains(t, out, ansiWrap("•", pal, colorGreen, false, false))
	assert.NotContains(t, stripANSI(out), "└")
}

func TestDeleteToolCompleteErrorUsesSharedFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)

	call := llmstream.ToolCall{
		Name:  "delete",
		Input: `{"path":"foo/old.txt"}`,
	}
	result := llmstream.ToolResult{
		Result:  "delete failed",
		IsError: true,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       newDeleteTool(t),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 90)
	require.NotEmpty(t, out)
	require.Equal(t, []string{
		"• Delete foo/old.txt",
		"  └ Error: delete failed",
	}, strings.Split(stripANSI(out), "\n"))
	assert.Contains(t, out, ansiWrap("•", pal, colorRed, false, false))
}

func TestDeleteToolCompleteOutsidePackageUsesSharedFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)

	call := llmstream.ToolCall{
		Name:  "delete",
		Input: `{"path":"foo/old.txt"}`,
	}
	result := llmstream.ToolResult{
		Result:    "denied",
		IsError:   true,
		SourceErr: authdomain.ErrCodeUnitPathOutside,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       newDeleteTool(t),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 90)
	require.NotEmpty(t, out)
	require.Equal(t, "• Silly LLM tried delete on foo/old.txt outside of package.", stripANSI(out))
	assert.Contains(t, out, ansiWrap("•", pal, colorRed, false, false))
}
func TestEditToolCompleteErrorShowsMessage(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "edit",
		Input: `{"path":"foo/bar.go","old_string":"old line","new_string":"new line"}`,
	}
	result := llmstream.ToolResult{
		Result:  "replace failed",
		IsError: true,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       newEditTool(t),
		ToolCall:   &call,
		ToolResult: &result,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 90)
	require.NotEmpty(t, out)
	require.Equal(t, []string{
		"• Edit foo/bar.go",
		"     - old line",
		"     + new line",
		"  └ Error: replace failed",
	}, strings.Split(stripANSI(out), "\n"))
	assert.Contains(t, out, ansiWrap("•", pal, colorRed, false, false))
}

func TestApplyPatchCompleteWithError(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	patch := `*** Begin Patch
*** Update File: foo/bar.go
@@
- old line
+ new line
*** End Patch
`
	call := llmstream.ToolCall{
		Name:  "apply_patch",
		Input: patch,
	}
	result := llmstream.ToolResult{
		Result:  "patch failed",
		IsError: true,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       newApplyPatchTool(t),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 80)
	require.NotEmpty(t, out)
	stripped := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Edit foo/bar.go",
		"     - old line",
		"     + new line",
		"  └ Error: patch failed",
	}, stripped)
	assert.Contains(t, out, ansiWrap("•", pal, colorRed, false, false))
	assert.Contains(t, out, ansiWrap("Error: patch failed", pal, colorRed, false, false))
}

func TestApplyPatchCompleteInvalidPatchIsConcise(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	formatter := NewTUIFormatter(cfg)

	invalidPatch := `*** Update File: foo/bar.go
@@
- old line
+ new line
*** End Patch
`
	_, err := applypatch.ApplyPatch(t.TempDir(), invalidPatch)
	require.Error(t, err)
	require.True(t, applypatch.IsInvalidPatch(err))

	call := llmstream.ToolCall{
		Name:  "apply_patch",
		Input: invalidPatch,
	}
	result := llmstream.ToolResult{
		// Simulate a tool error that might include the patch text. We should never render it for invalid patches.
		Result:    "invalid patch:\n" + invalidPatch,
		IsError:   true,
		SourceErr: err,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       newApplyPatchTool(t),
		ToolCall:   &call,
		ToolResult: &result,
	}

	t.Run("tui", func(t *testing.T) {
		out := formatter.FormatEvent(event, 120)
		require.NotEmpty(t, out)
		lines := strings.Split(stripANSI(out), "\n")
		require.Equal(t, []string{
			"• Edit foo/bar.go",
			"  └ Failed: LLM supplied an invalid patch.",
		}, lines)
		assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorRed, false, false)+" "))
		assert.NotContains(t, stripANSI(out), "*** Update File:")
		assert.NotContains(t, stripANSI(out), "old line")
	})

	t.Run("cli", func(t *testing.T) {
		out := formatter.FormatEvent(event, MinTerminalWidth)
		require.NotEmpty(t, out)
		lines := strings.Split(stripANSI(out), "\n")
		require.Equal(t, []string{
			"• Edit foo/bar.go",
			"  └ Failed: LLM supplied an invalid patch.",
		}, lines)
		assert.NotContains(t, stripANSI(out), "*** Update File:")
		assert.NotContains(t, stripANSI(out), "old line")
	})
}

func TestUpdatePlanCallFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)

	input := `{
  "explanation": "Need to align CodeUnit authorizer with updated SPEC behavior for read-only restrictions and adjust tests accordingly.",
  "plan": [
    {"step":"Inspect SPEC changes and current CodeUnit authorizer implementation","status":"completed"},
    {"step":"Update codeunit authorizer logic to apply read restrictions only to read_file tool and keep write restrictions for all tools","status":"pending"},
    {"step":"Revise tests to cover new behavior and run go test for package","status":"pending"}
  ]
}`
	call := llmstream.ToolCall{
		Name:  "update_plan",
		Input: input,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     newUpdatePlanTool(t),
		ToolCall: &call,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 400)
	require.NotEmpty(t, out)

	// Check header formatting and lines.
	lines := strings.Split(stripANSI(out), "\n")
	require.GreaterOrEqual(t, len(lines), 5)
	assert.Equal(t, "• Update Plan", lines[0])
	assert.True(t, strings.HasPrefix(lines[1], "  └ "), "first detail line should start with └")
	assert.Contains(t, lines[1], "Need to align CodeUnit authorizer")
	assert.Equal(t, "    ✔ Inspect SPEC changes and current CodeUnit authorizer implementation", lines[2])
	assert.Equal(t, "    □ Update codeunit authorizer logic to apply read restrictions only to read_file tool and keep write restrictions for all tools", lines[3])
	assert.Equal(t, "    □ Revise tests to cover new behavior and run go test for package", lines[4])

	// Check coloring: header should be colorful+bold, first uncompleted todo should be colorful (not accent).
	assert.Contains(t, out, ansiWrap("Update Plan", pal, colorColorful, false, true), "header should be colorful and bold")
	rawLines := strings.Split(out, "\n")
	require.GreaterOrEqual(t, len(rawLines), 4)
	colorfulPrefix := pal.style(runeStyle{color: colorColorful}).OpeningControlCodes()
	assert.Contains(t, rawLines[3], colorfulPrefix, "first uncompleted todo should be colorful")
}

func TestUpdatePlanCompleteSuccess(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name: "update_plan",
		Input: `{
  "explanation": "",
  "plan": [
    {"step":"Do thing A","status":"completed"},
    {"step":"Do thing B","status":"pending"}
  ]
}`,
	}
	// Simulate a successful completion.
	result := llmstream.ToolResult{
		Result:  `{"success":true}`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       newUpdatePlanTool(t),
		ToolCall:   &call,
		ToolResult: &result,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 80)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Update Plan",
		"  └ ✔ Do thing A",
		"    □ Do thing B",
	}, lines)
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorGreen, false, false)+" "), "bullet should be green on success")
}

func TestUpdatePlanNoExplanationStartsWithFirstItem(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	call := llmstream.ToolCall{
		Name: "update_plan",
		Input: `{
  "explanation": "",
  "plan": [
    {"step":"First","status":"pending"}
  ]
}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     newUpdatePlanTool(t),
		ToolCall: &call,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 80)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Update Plan",
		"  └ □ First",
	}, lines)
}

func TestUpdateUsageCallFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name: "update_usage",
		Input: `{
  "instructions": "Update the callsites to conform to this new API.",
  "paths": ["some/path", "other/path", "third/path", "fourth/path", "fifth/path", "sixth/path", "seventh/path"]
}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     pkgToolWithPresenter(t, newUpdateUsageTool(t)),
		ToolCall: &call,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 160)
	require.NotEmpty(t, out)

	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Updating Usage in some/path, other/path, third/path (4 more)",
		"  └ Update the callsites to conform to this new API.",
	}, lines)

	assert.Contains(t, out, ansiWrap("Updating Usage", pal, colorColorful, false, true), "verb should be colorful and bold")
	assert.Contains(t, out, ansiWrap(" in", pal, colorAccent, false, false), "`in` keyword should be accented")
	assert.Contains(t, out, ansiWrap(" (4 more)", pal, colorAccent, false, false), "parenthetical should be accented")
}

func TestUpdateUsageCompleteSuccess(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name: "update_usage",
		Input: `{
  "instructions": "Do not repeat instructions on complete.",
  "paths": ["first/path", "second/path", "third/path", "fourth/path", "fifth/path"]
}`,
	}
	result := llmstream.ToolResult{
		Result:  `{"success":true}`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       pkgToolWithPresenter(t, newUpdateUsageTool(t)),
		ToolCall:   &call,
		ToolResult: &result,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 160)
	require.NotEmpty(t, out)

	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Updated Usage in first/path, second/path, third/path (2 more)",
	}, lines)
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorGreen, false, false)+" "), "success bullet should be green")
	assert.NotContains(t, out, "└", "success output should not include a body")
}

func TestUpdateUsageCompleteErrorShowsOutput(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name: "update_usage",
		Input: `{
  "instructions": "Update the callsites to conform to this new API.",
  "paths": ["pkg/one", "pkg/two", "pkg/three"]
}`,
	}
	result := llmstream.ToolResult{
		Result:  "failed to update usage",
		IsError: true,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       pkgToolWithPresenter(t, newUpdateUsageTool(t)),
		ToolCall:   &call,
		ToolResult: &result,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 160)
	require.NotEmpty(t, out)

	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Updated Usage in pkg/one, pkg/two, pkg/three",
		"  └ Error: failed to update usage",
	}, lines)
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorRed, false, false)+" "), "failure should use red bullet")
}

func TestChangeAPICallFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	sandbox := t.TempDir()
	call := llmstream.ToolCall{
		Name: "change_api",
		Input: `{
  "path": "axi/some/pkg",
  "instructions": "Add a new method SomeType.DoThing so downstream callers can avoid duplicating this logic."
}`,
	}
	event := agent.Event{
		Type: agent.EventTypeToolCall,
		Tool: extToolWithPresenter(t, pkgtools.NewChangeAPITool(
			sandbox,
			authdomain.NewAutoApproveAuthorizer(sandbox),
			nil,
			llmmodel.DefaultModel,
			nil,
		)),
		ToolCall: &call,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 160)
	require.NotEmpty(t, out)

	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Changing API in axi/some/pkg",
		"  └ Add a new method SomeType.DoThing so downstream callers can avoid duplicating this logic.",
	}, lines)

	assert.Contains(t, out, ansiWrap("Changing API", pal, colorColorful, false, true), "verb should be colorful and bold")
	assert.Contains(t, out, ansiWrap(" in", pal, colorAccent, false, false), "`in` keyword should be accented")
}

func TestChangeAPICompleteSuccess(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	sandbox := t.TempDir()
	call := llmstream.ToolCall{
		Name: "change_api",
		Input: `{
  "path": "axi/some/pkg",
  "instructions": "Do not repeat instructions on complete."
}`,
	}
	result := llmstream.ToolResult{
		Result:  `{"success":true}`,
		IsError: false,
	}
	event := agent.Event{
		Type: agent.EventTypeToolComplete,
		Tool: extToolWithPresenter(t, pkgtools.NewChangeAPITool(
			sandbox,
			authdomain.NewAutoApproveAuthorizer(sandbox),
			nil,
			llmmodel.DefaultModel,
			nil,
		)),
		ToolCall:   &call,
		ToolResult: &result,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 160)
	require.NotEmpty(t, out)

	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Changed API in axi/some/pkg",
	}, lines)
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorGreen, false, false)+" "), "success bullet should be green")
	assert.NotContains(t, out, "└", "success output should not include a body")
}

func TestChangeAPICompleteErrorShowsOutput(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	sandbox := t.TempDir()
	call := llmstream.ToolCall{
		Name: "change_api",
		Input: `{
  "path": "axi/some/pkg",
  "instructions": "Add method."
}`,
	}
	result := llmstream.ToolResult{
		Result:  "failed to change api",
		IsError: true,
	}
	event := agent.Event{
		Type: agent.EventTypeToolComplete,
		Tool: extToolWithPresenter(t, pkgtools.NewChangeAPITool(
			sandbox,
			authdomain.NewAutoApproveAuthorizer(sandbox),
			nil,
			llmmodel.DefaultModel,
			nil,
		)),
		ToolCall:   &call,
		ToolResult: &result,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 160)
	require.NotEmpty(t, out)

	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Changed API in axi/some/pkg",
		"  └ Error: failed to change api",
	}, lines)
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorRed, false, false)+" "), "failure should use red bullet")
}

func TestReviewToolCallFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	formatter := NewTUIFormatter(cfg)

	call := llmstream.ToolCall{
		Name:  "review",
		Input: `{"base":"origin/main"}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     testTool("review"),
		ToolCall: &call,
	}

	t.Run("tui", func(t *testing.T) {
		out := formatter.FormatEvent(event, 120)
		require.NotEmpty(t, out)
		require.Equal(t, "• Reviewing origin/main", stripANSI(out))
		assert.Contains(t, out, ansiWrap("Reviewing", pal, colorColorful, false, true))
		assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorAccent, false, false)+" "))
		assert.NotContains(t, stripANSI(out), "└")
	})

	t.Run("cli", func(t *testing.T) {
		out := formatter.FormatEvent(event, MinTerminalWidth)
		require.NotEmpty(t, out)
		require.Equal(t, "• Reviewing origin/main", stripANSI(out))
		assert.NotContains(t, stripANSI(out), "review {")
	})
}

func builtInReviewToolPayload() map[string]any {
	return map[string]any{
		"findings": []map[string]any{
			{
				"title":            "[P2] Return JSON payload",
				"body":             "The orchestrator expects JSON back from review so this must stay machine-readable.",
				"confidence_score": 0.81,
				"priority":         2,
				"code_location": map[string]any{
					"absolute_file_path": "/tmp/review/pkg.go",
					"line_range": map[string]any{
						"start": 1,
						"end":   1,
					},
				},
			},
		},
		"overall_correctness":      "patch is incorrect",
		"overall_explanation":      "The patch still has one actionable issue.",
		"overall_confidence_score": 0.81,
	}
}

func TestSubagentReviewJSONAssistantTextIsSuppressed(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)

	payload := builtInReviewToolPayload()
	data, err := json.Marshal(payload)
	require.NoError(t, err)

	assistantEvent := agent.Event{
		Agent: agent.AgentMeta{Depth: 1},
		Type:  agent.EventTypeAssistantText,
		TextContent: llmstream.TextContent{
			Content: string(data),
		},
	}

	reviewCall := llmstream.ToolCall{
		Name:  "review",
		Input: `{"base":"origin/main"}`,
	}
	reviewResult := llmstream.ToolResult{
		Result:  string(data),
		IsError: false,
	}
	reviewEvent := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testTool("review"),
		ToolCall:   &reviewCall,
		ToolResult: &reviewResult,
	}

	expectedReviewLines := []string{
		"• Reviewed origin/main",
		"  └ [P2] Return JSON payload",
	}

	t.Run("tui", func(t *testing.T) {
		assistantOut := formatter.FormatEvent(assistantEvent, 200)
		require.Empty(t, stripANSI(assistantOut))

		reviewOut := formatter.FormatEvent(reviewEvent, 200)
		require.Equal(t, expectedReviewLines, strings.Split(stripANSI(reviewOut), "\n"))
		assert.NotContains(t, stripANSI(reviewOut), `"overall_correctness"`)
	})

	t.Run("cli", func(t *testing.T) {
		assistantOut := formatter.FormatEvent(assistantEvent, MinTerminalWidth)
		require.Empty(t, stripANSI(assistantOut))

		reviewOut := formatter.FormatEvent(reviewEvent, MinTerminalWidth)
		require.Equal(t, expectedReviewLines, strings.Split(stripANSI(reviewOut), "\n"))
		assert.NotContains(t, stripANSI(reviewOut), `"overall_correctness"`)
	})
}

func TestSubagentNonReviewAssistantTextStillRenders(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)

	event := agent.Event{
		Agent: agent.AgentMeta{Depth: 1},
		Type:  agent.EventTypeAssistantText,
		TextContent: llmstream.TextContent{
			Content: `{"hello":"world"}`,
		},
	}

	t.Run("tui", func(t *testing.T) {
		out := formatter.FormatEvent(event, 200)
		require.NotEmpty(t, out)
		assert.Contains(t, stripANSI(out), `{"hello":"world"}`)
	})

	t.Run("cli", func(t *testing.T) {
		out := formatter.FormatEvent(event, MinTerminalWidth)
		require.NotEmpty(t, out)
		assert.Contains(t, stripANSI(out), `{"hello":"world"}`)
	})
}

func TestReviewToolCompleteFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	formatter := NewTUIFormatter(cfg)

	call := llmstream.ToolCall{
		Name:  "review",
		Input: `{"base":"origin/main"}`,
	}

	assertBothModes := func(t *testing.T, event agent.Event, tuiExpected []string, cliExpected []string) {
		t.Helper()

		t.Run("tui", func(t *testing.T) {
			out := formatter.FormatEvent(event, 200)
			require.NotEmpty(t, out)
			require.Equal(t, tuiExpected, strings.Split(stripANSI(out), "\n"))
		})

		t.Run("cli", func(t *testing.T) {
			out := formatter.FormatEvent(event, MinTerminalWidth)
			require.NotEmpty(t, out)
			require.Equal(t, cliExpected, strings.Split(stripANSI(out), "\n"))
		})
	}

	t.Run("success with findings", func(t *testing.T) {
		payload := builtInReviewToolPayload()
		findings := append(payload["findings"].([]map[string]any), []map[string]any{
			{
				"title":            "[P1] internal/agentbuilder: YAML package-target resolution falls back to a missing module root for generic callers.",
				"body":             "Detailed explanation that should not be shown.",
				"confidence_score": 0.9,
				"priority":         1,
				"code_location": map[string]any{
					"absolute_file_path": "/tmp/review/internal/agentbuilder/config.go",
					"line_range": map[string]any{
						"start": 41,
						"end":   41,
					},
				},
			},
			{
				"title":            "[P1] internal/agentformatter: review JSON is still rendered as raw payload text.",
				"body":             "Another body that should be ignored.",
				"confidence_score": 0.92,
				"priority":         1,
				"code_location": map[string]any{
					"absolute_file_path": "/tmp/review/internal/agentformatter/agentformatter.go",
					"line_range": map[string]any{
						"start": 2964,
						"end":   2986,
					},
				},
			},
			{
				"title":            "[P2] internal/agentbuilder: review prompt file path is not validated before read.",
				"confidence_score": 0.73,
				"priority":         2,
				"code_location": map[string]any{
					"absolute_file_path": "/tmp/review/internal/agentbuilder/prompt.go",
					"line_range": map[string]any{
						"start": 10,
						"end":   10,
					},
				},
			},
			{
				"title":            "[P2] internal/orchestrate: review errors are swallowed on retry.",
				"confidence_score": 0.72,
				"priority":         2,
				"code_location": map[string]any{
					"absolute_file_path": "/tmp/review/internal/orchestrate/retry.go",
					"line_range": map[string]any{
						"start": 22,
						"end":   22,
					},
				},
			},
			{
				"title":            "[P3] internal/tui: completion banner wraps awkwardly for narrow terminals.",
				"confidence_score": 0.66,
				"priority":         3,
				"code_location": map[string]any{
					"absolute_file_path": "/tmp/review/internal/tui/banner.go",
					"line_range": map[string]any{
						"start": 8,
						"end":   8,
					},
				},
			},
			{
				"title":            "[P3] internal/noninteractive: status line omits review summary.",
				"confidence_score": 0.61,
				"priority":         3,
				"code_location": map[string]any{
					"absolute_file_path": "/tmp/review/internal/noninteractive/output.go",
					"line_range": map[string]any{
						"start": 17,
						"end":   17,
					},
				},
			},
		}...)
		payload["findings"] = findings
		data, err := json.Marshal(payload)
		require.NoError(t, err)

		result := llmstream.ToolResult{
			Result:  string(data),
			IsError: false,
		}
		event := agent.Event{
			Type:       agent.EventTypeToolComplete,
			Tool:       testTool("review"),
			ToolCall:   &call,
			ToolResult: &result,
		}

		expected := []string{
			"• Reviewed origin/main",
			"  └ [P2] Return JSON payload",
			"    [P1] internal/agentbuilder: YAML package-target resolution falls back to a missing module root for generic callers.",
			"    [P1] internal/agentformatter: review JSON is still rendered as raw payload text.",
			"    [P2] internal/agentbuilder: review prompt file path is not validated before read.",
			"    [P2] internal/orchestrate: review errors are swallowed on retry.",
			"    … +2 findings",
		}
		assertBothModes(t, event, expected, expected)

		out := formatter.FormatEvent(event, 200)
		assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorGreen, false, false)+" "))
		assert.Contains(t, out, ansiWrap("Reviewed", pal, colorColorful, false, true))
		assert.NotContains(t, stripANSI(out), "Detailed explanation that should not be shown.")
		assert.NotContains(t, stripANSI(out), "overall_correctness")
	})

	t.Run("success with wrapped built-in findings payload", func(t *testing.T) {
		payload := builtInReviewToolPayload()
		resultPayload := map[string]any{
			"success": true,
			"content": payload,
		}
		data, err := json.Marshal(resultPayload)
		require.NoError(t, err)

		result := llmstream.ToolResult{
			Result:  string(data),
			IsError: false,
		}
		event := agent.Event{
			Type:       agent.EventTypeToolComplete,
			Tool:       testTool("review"),
			ToolCall:   &call,
			ToolResult: &result,
		}

		expected := []string{
			"• Reviewed origin/main",
			"  └ [P2] Return JSON payload",
		}
		assertBothModes(t, event, expected, expected)

		out := formatter.FormatEvent(event, 200)
		assert.NotContains(t, stripANSI(out), `"findings"`)
		assert.NotContains(t, stripANSI(out), `"overall_correctness"`)
	})

	t.Run("success with no findings", func(t *testing.T) {
		payload := map[string]any{
			"findings":                 []map[string]any{},
			"overall_correctness":      "patch is correct",
			"overall_explanation":      "The patch looks correct and no actionable findings were identified.",
			"overall_confidence_score": 0.88,
		}
		data, err := json.Marshal(payload)
		require.NoError(t, err)

		result := llmstream.ToolResult{
			Result:  string(data),
			IsError: false,
		}
		event := agent.Event{
			Type:       agent.EventTypeToolComplete,
			Tool:       testTool("review"),
			ToolCall:   &call,
			ToolResult: &result,
		}

		expected := []string{
			"• Reviewed origin/main",
			"  └ No findings. Patch is correct.",
		}
		assertBothModes(t, event, expected, expected)
	})

	t.Run("error shows message", func(t *testing.T) {
		result := llmstream.ToolResult{
			Result:  "review failed",
			IsError: true,
		}
		event := agent.Event{
			Type:       agent.EventTypeToolComplete,
			Tool:       testTool("review"),
			ToolCall:   &call,
			ToolResult: &result,
		}

		expected := []string{
			"• Reviewed origin/main",
			"  └ Error: review failed",
		}
		assertBothModes(t, event, expected, expected)

		out := formatter.FormatEvent(event, 120)
		assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorRed, false, false)+" "))
	})

	t.Run("malformed or non-review json falls back", func(t *testing.T) {
		testCases := []struct {
			name    string
			payload string
		}{
			{
				name:    "non-review json",
				payload: `{"hello":"world"}`,
			},
			{
				name:    "malformed json",
				payload: `{"hello"`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result := llmstream.ToolResult{
					Result:  tc.payload,
					IsError: false,
				}
				event := agent.Event{
					Type:       agent.EventTypeToolComplete,
					Tool:       testTool("review"),
					ToolCall:   &call,
					ToolResult: &result,
				}

				expected := []string{
					"• Reviewed origin/main",
					"  └ " + tc.payload,
				}
				assertBothModes(t, event, expected, expected)
			})
		}
	})
}

func TestImplementToolCallFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	formatter := NewTUIFormatter(cfg)

	call := llmstream.ToolCall{
		Name:  "implement",
		Input: `{"path":"internal/agentformatter","instructions":"Format the new orchestrator implement/review events so manual and noninteractive output stays readable."}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     testTool("implement"),
		ToolCall: &call,
	}

	t.Run("tui", func(t *testing.T) {
		out := formatter.FormatEvent(event, 160)
		require.NotEmpty(t, out)
		lines := strings.Split(stripANSI(out), "\n")
		require.Equal(t, []string{
			"• Implementing internal/agentformatter",
			"  └ Format the new orchestrator implement/review events so manual and noninteractive output stays readable.",
		}, lines)
		assert.Contains(t, out, ansiWrap("Implementing", pal, colorColorful, false, true))
		assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorAccent, false, false)+" "))
	})

	t.Run("cli", func(t *testing.T) {
		out := formatter.FormatEvent(event, MinTerminalWidth)
		require.NotEmpty(t, out)
		lines := strings.Split(stripANSI(out), "\n")
		require.Equal(t, []string{
			"• Implementing internal/agentformatter",
			"  └ Format the new orchestrator implement/review events so manual and noninteractive output stays readable.",
		}, lines)
		assert.NotContains(t, stripANSI(out), "subagent")
	})
}

func TestImplementToolCompleteFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	formatter := NewTUIFormatter(cfg)

	call := llmstream.ToolCall{
		Name:  "implement",
		Input: `{"path":"internal/agentformatter","instructions":"Format the new orchestrator implement/review events so manual and noninteractive output stays readable."}`,
	}

	t.Run("success with summarized output", func(t *testing.T) {
		result := llmstream.ToolResult{
			Result:  `{"success":true,"content":"Added focused coverage for orchestrator tool-event formatting."}`,
			IsError: false,
		}
		event := agent.Event{
			Type:       agent.EventTypeToolComplete,
			Tool:       testTool("implement"),
			ToolCall:   &call,
			ToolResult: &result,
		}

		t.Run("tui", func(t *testing.T) {
			out := formatter.FormatEvent(event, 160)
			require.NotEmpty(t, out)
			lines := strings.Split(stripANSI(out), "\n")
			require.Equal(t, []string{
				"• Implemented internal/agentformatter",
				"  └ Added focused coverage for orchestrator tool-event formatting.",
			}, lines)
			assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorGreen, false, false)+" "))
			assert.Contains(t, out, ansiWrap("Implemented", pal, colorColorful, false, true))
		})

		t.Run("cli", func(t *testing.T) {
			out := formatter.FormatEvent(event, MinTerminalWidth)
			require.NotEmpty(t, out)
			lines := strings.Split(stripANSI(out), "\n")
			require.Equal(t, []string{
				"• Implemented internal/agentformatter",
				"  └ Added focused coverage for orchestrator tool-event formatting.",
			}, lines)
		})
	})

	t.Run("error shows message", func(t *testing.T) {
		result := llmstream.ToolResult{
			Result:  "implementation failed",
			IsError: true,
		}
		event := agent.Event{
			Type:       agent.EventTypeToolComplete,
			Tool:       testTool("implement"),
			ToolCall:   &call,
			ToolResult: &result,
		}

		t.Run("tui", func(t *testing.T) {
			out := formatter.FormatEvent(event, 120)
			require.NotEmpty(t, out)
			lines := strings.Split(stripANSI(out), "\n")
			require.Equal(t, []string{
				"• Implemented internal/agentformatter",
				"  └ Error: implementation failed",
			}, lines)
			assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorRed, false, false)+" "))
		})

		t.Run("cli", func(t *testing.T) {
			out := formatter.FormatEvent(event, MinTerminalWidth)
			require.NotEmpty(t, out)
			lines := strings.Split(stripANSI(out), "\n")
			require.Equal(t, []string{
				"• Implemented internal/agentformatter",
				"  └ Error: implementation failed",
			}, lines)
		})
	})
}

func TestImplementToolCompleteDoesNotTruncateOutput(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)

	call := llmstream.ToolCall{
		Name:  "implement",
		Input: `{"path":"internal/agentformatter","instructions":"Update formatter output."}`,
	}
	result := llmstream.ToolResult{
		Result:  `{"success":true,"content":"line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7"}`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testTool("implement"),
		ToolCall:   &call,
		ToolResult: &result,
	}

	expectedLines := []string{
		"• Implemented internal/agentformatter",
		"  └ line 1",
		"    line 2",
		"    line 3",
		"    line 4",
		"    line 5",
		"    line 6",
		"    line 7",
	}

	t.Run("tui", func(t *testing.T) {
		out := formatter.FormatEvent(event, 160)
		require.NotEmpty(t, out)
		require.Equal(t, expectedLines, strings.Split(stripANSI(out), "\n"))
		assert.NotContains(t, stripANSI(out), "… +")
	})

	t.Run("cli", func(t *testing.T) {
		out := formatter.FormatEvent(event, MinTerminalWidth)
		require.NotEmpty(t, out)
		require.Equal(t, expectedLines, strings.Split(stripANSI(out), "\n"))
		assert.NotContains(t, stripANSI(out), "… +")
	})
}

func TestModuleInfoToolCallNoOptions(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "module_info",
		Input: `{}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     testTool("module_info"),
		ToolCall: &call,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 120)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Read Module Info",
	}, lines)
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorAccent, false, false)+" "), "in-progress bullet should be accent")
	assert.Contains(t, out, ansiWrap("Read Module Info", pal, colorColorful, false, true), "header should be bold and colorful")
}

func TestModuleInfoToolCallWithOptions(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	call := llmstream.ToolCall{
		Name:  "module_info",
		Input: `{"package_search":"agentformatter","include_dependency_packages":true}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     testTool("module_info"),
		ToolCall: &call,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 120)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Read Module Info",
		"  └ Search: agentformatter; Deps: true",
	}, lines)
}

func TestModuleInfoToolCompleteSuccessMirrorsCall(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "module_info",
		Input: `{"package_search":"agentformatter","include_dependency_packages":true}`,
	}
	result := llmstream.ToolResult{
		Result:  `{"success":true,"content":"(big payload elided)"}`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testTool("module_info"),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 120)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Read Module Info",
		"  └ Search: agentformatter; Deps: true",
	}, lines)
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorGreen, false, false)+" "), "success bullet should be green")
}

func TestModuleInfoToolCompleteErrorDoesNotPrintToolOutput(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "module_info",
		Input: `{"package_search":"agentformatter"}`,
	}
	result := llmstream.ToolResult{
		Result:  "go mod parse error",
		IsError: true,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testTool("module_info"),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 120)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Read Module Info",
		"  └ Search: agentformatter",
	}, lines)
	assert.NotContains(t, stripANSI(out), "Error:", "module_info completion should not include tool output per SPEC")
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorRed, false, false)+" "), "failure bullet should be red")
}

func TestGetPublicAPICallWithIdentifiers(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "get_public_api",
		Input: `{"path":"axi/some/pkg","identifiers":["SomeType","DoThingFunc"]}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     testTool("get_public_api"),
		ToolCall: &call,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 100)
	require.NotEmpty(t, out)
	// Check header and identifiers line (stripped).
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Read Public API axi/some/pkg",
		"  └ SomeType, DoThingFunc",
	}, lines)
	// Header verb should be colorful and bold.
	assert.Contains(t, out, ansiWrap("Read Public API", pal, colorColorful, false, true))
}

func TestGetPublicAPICompleteSuccessWithIdentifiers(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "get_public_api",
		Input: `{"path":"axi/another/pkg","identifiers":["T","F"]}`,
	}
	result := llmstream.ToolResult{
		Result:  `{"success":true}`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testTool("get_public_api"),
		ToolCall:   &call,
		ToolResult: &result,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 120)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Read Public API axi/another/pkg",
		"  └ T, F",
	}, lines)
	// Bullet should be green on success.
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorGreen, false, false)+" "))
}

func TestGetPublicAPICompleteErrorShowsMessage(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "get_public_api",
		Input: `{"path":"axi/bad/pkg","identifiers":["X"]}`,
	}
	result := llmstream.ToolResult{
		Result:  "package not found",
		IsError: true,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testTool("get_public_api"),
		ToolCall:   &call,
		ToolResult: &result,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 120)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Read Public API axi/bad/pkg",
		"  └ Error: package not found",
	}, lines)
	// Bullet should be red on error.
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorRed, false, false)+" "))
}

func TestClarifyPublicAPICallFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "clarify_public_api",
		Input: `{"path":"axi/some/pkg","identifier":"SomeIdentifier","question":"What does SomeIdentifier return?"}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     pkgToolWithPresenter(t, newClarifyPublicAPITool(t)),
		ToolCall: &call,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 120)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Clarifying API SomeIdentifier in axi/some/pkg",
		"  └ What does SomeIdentifier return?",
	}, lines)
	assert.Contains(t, out, ansiWrap("Clarifying API", pal, colorColorful, false, true))
	assert.Contains(t, out, ansiWrap(" in", pal, colorAccent, false, false))
}

func TestClarifyPublicAPICompleteSuccess(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "clarify_public_api",
		Input: `{"path":"axi/another/pkg","identifier":"TType","question":"Explain TType usage."}`,
	}
	resultPayload := map[string]any{
		"success": true,
		"content": "TType returns detailed metadata describing the entity.",
	}
	data, err := json.Marshal(resultPayload)
	require.NoError(t, err)
	result := llmstream.ToolResult{
		Result:  string(data),
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       pkgToolWithPresenter(t, newClarifyPublicAPITool(t)),
		ToolCall:   &call,
		ToolResult: &result,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 140)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Clarified API TType in axi/another/pkg",
		"  └ TType returns detailed metadata describing the entity.",
	}, lines)
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorGreen, false, false)+" "))
}

func TestClarifyPublicAPICompleteError(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "clarify_public_api",
		Input: `{"path":"axi/bad/pkg","identifier":"Missing","question":"Explain Missing."}`,
	}
	result := llmstream.ToolResult{
		Result:  "identifier not found",
		IsError: true,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       pkgToolWithPresenter(t, newClarifyPublicAPITool(t)),
		ToolCall:   &call,
		ToolResult: &result,
	}
	out := NewTUIFormatter(cfg).FormatEvent(event, 140)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Clarified API Missing in axi/bad/pkg",
		"  └ Error: identifier not found",
	}, lines)
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorRed, false, false)+" "))
}

func TestGetUsageToolCallFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "get_usage",
		Input: `{"defining_package_path":"axi/some/pkg","identifier":"*SomeType.SomeFunc"}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     testTool("get_usage"),
		ToolCall: &call,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 120)
	require.NotEmpty(t, out)
	require.Equal(t, "• Read Usage axi/some/pkg *SomeType.SomeFunc", stripANSI(out))
	assert.Contains(t, out, ansiWrap("Read Usage", pal, colorColorful, false, true))
}

func TestGetUsageToolCompleteSuccessCountsResults(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "get_usage",
		Input: `{"defining_package_path":"axi/some/pkg","identifier":"*SomeType.SomeFunc"}`,
	}
	// Results are counted by matching /^\d+:/.
	content := "1: first\nSome details\n2: second\n3: third"
	resultPayload := map[string]any{
		"success": true,
		"content": content,
	}
	data, err := json.Marshal(resultPayload)
	require.NoError(t, err)
	result := llmstream.ToolResult{
		Result:  string(data),
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       testTool("get_usage"),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 120)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Read Usage axi/some/pkg *SomeType.SomeFunc",
		"  └ Found 3 results.",
	}, lines)
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorGreen, false, false)+" "), "success bullet should be green")
}

func TestGetUsageToolCallIgnoresLegacyParams(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}

	formatter := NewTUIFormatter(cfg)

	t.Run("import_path is ignored", func(t *testing.T) {
		call := llmstream.ToolCall{
			Name:  "get_usage",
			Input: `{"import_path":"axi/some/pkg","identifier":"*SomeType.SomeFunc"}`,
		}
		event := agent.Event{
			Type:     agent.EventTypeToolCall,
			ToolCall: &call,
		}

		out := formatter.FormatEvent(event, 120)
		require.NotEmpty(t, out)
		assert.Equal(t, "• Read Usage get_usage *SomeType.SomeFunc", stripANSI(out))
	})

	t.Run("path is ignored", func(t *testing.T) {
		call := llmstream.ToolCall{
			Name:  "get_usage",
			Input: `{"path":"axi/some/pkg","identifier":"*SomeType.SomeFunc"}`,
		}
		event := agent.Event{
			Type:     agent.EventTypeToolCall,
			ToolCall: &call,
		}

		out := formatter.FormatEvent(event, 120)
		require.NotEmpty(t, out)
		assert.Equal(t, "• Read Usage get_usage *SomeType.SomeFunc", stripANSI(out))
	})
}

func TestToolNamePrecedence(t *testing.T) {
	t.Run("prefers tool object name", func(t *testing.T) {
		event := agent.Event{
			Tool: testTool("skill_shell"),
			ToolCall: &llmstream.ToolCall{
				Name: "read_file",
			},
			ToolResult: &llmstream.ToolResult{
				Name: "ls",
			},
		}

		assert.Equal(t, "shell", normalizedToolName(event))
		assert.Equal(t, "skill_shell", toolDisplayName(event))
	})

	t.Run("falls back to tool call name", func(t *testing.T) {
		event := agent.Event{
			ToolCall: &llmstream.ToolCall{
				Name: "read_file",
			},
			ToolResult: &llmstream.ToolResult{
				Name: "ls",
			},
		}

		assert.Equal(t, "read_file", normalizedToolName(event))
		assert.Equal(t, "read_file", toolDisplayName(event))
	})

	t.Run("falls back to tool result name", func(t *testing.T) {
		event := agent.Event{
			ToolResult: &llmstream.ToolResult{
				Name: "diagnostics",
			},
		}

		assert.Equal(t, "diagnostics", normalizedToolName(event))
		assert.Equal(t, "diagnostics", toolDisplayName(event))
	})
}

func TestAssistantTextWrapsWideRunes(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg).(*textTUIFormatter)
	out := formatter.tuiAssistantText("你你你你", 8)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• 你你你",
		"  你",
	}, lines)
}

func TestRunTestsCompleteSuccessShowsConciseStatusLine(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	formatter := NewTUIFormatter(cfg)

	call := llmstream.ToolCall{
		Name:  "run_tests",
		Input: `{"path":"./internal/tools/toolsets"}`,
	}
	content := `<test-status ok="true">
$ go test ./internal/tools/toolsets
ok  	github.com/codalotl/codalotl/internal/tools/toolsets	(cached)
</test-status>
<lint-status ok="true">
$ staticcheck ./internal/tools/toolsets
</lint-status>`
	result := llmstream.ToolResult{
		Result:  content,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       extToolWithPresenter(t, exttools.NewRunTestsTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), nil)),
		ToolCall:   &call,
		ToolResult: &result,
	}

	t.Run("tui", func(t *testing.T) {
		out := formatter.FormatEvent(event, 140)
		require.NotEmpty(t, out)
		lines := strings.Split(stripANSI(out), "\n")
		require.Equal(t, []string{
			"• Ran Tests ./internal/tools/toolsets",
			"  └ Tests: pass | Lints: pass",
		}, lines)
		assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorGreen, false, false)+" "))
		assert.NotContains(t, stripANSI(out), "<test-status")
		assert.NotContains(t, stripANSI(out), "<lint-status")
	})

	t.Run("cli", func(t *testing.T) {
		out := formatter.FormatEvent(event, MinTerminalWidth)
		require.NotEmpty(t, out)
		lines := strings.Split(stripANSI(out), "\n")
		require.Equal(t, []string{
			"• Ran Tests ./internal/tools/toolsets",
			"  └ Tests: pass | Lints: pass",
		}, lines)
	})
}

func TestRunTestsCompleteLintFailureShowsConciseStatusOnly(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	formatter := NewTUIFormatter(cfg)

	call := llmstream.ToolCall{
		Name:  "run_tests",
		Input: `{"path":"./internal/tools/toolsets"}`,
	}
	content := `<test-status ok="true">
$ go test ./internal/tools/toolsets
ok  	github.com/codalotl/codalotl/internal/tools/toolsets	(cached)
</test-status>
<lint-status ok="false">
$ gofmt -l -w internal/agentformatter
file1.go
file2.go
file3.go
file4.go
file5.go
file6.go
</lint-status>`
	result := llmstream.ToolResult{
		Result:  content,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       extToolWithPresenter(t, exttools.NewRunTestsTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), nil)),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := formatter.FormatEvent(event, 200)
	require.NotEmpty(t, out)
	stripped := stripANSI(out)
	lines := strings.Split(stripped, "\n")
	require.Equal(t, []string{
		"• Ran Tests ./internal/tools/toolsets",
		"  └ Tests: pass | Lints: fail",
	}, lines)
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorRed, false, false)+" "))
	assert.NotContains(t, stripped, "<test-status")
	assert.NotContains(t, stripped, "<lint-status")
	assert.NotContains(t, stripped, "$ go test")
	assert.NotContains(t, stripped, "$ gofmt")
}

func TestRunTestsCompleteMissingLintSectionUsesDashStatus(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	formatter := NewTUIFormatter(cfg)

	call := llmstream.ToolCall{
		Name:  "run_tests",
		Input: `{"path":"./internal/tools/toolsets"}`,
	}
	content := `<test-status ok="true">
$ go test ./internal/tools/toolsets
ok  	github.com/codalotl/codalotl/internal/tools/toolsets	(cached)
</test-status>`
	result := llmstream.ToolResult{
		Result:  content,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       extToolWithPresenter(t, exttools.NewRunTestsTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), nil)),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := formatter.FormatEvent(event, 200)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Ran Tests ./internal/tools/toolsets",
		"  └ Tests: pass | Lints: -",
	}, lines)
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorGreen, false, false)+" "), "overall success should be derived from the only present section")
}
func TestRunProjectTestsCallFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	formatter := NewTUIFormatter(cfg)
	call := llmstream.ToolCall{Name: "run_project_tests", Input: `{}`}
	event := agent.Event{Type: agent.EventTypeToolCall, Tool: extToolWithPresenter(t, exttools.NewRunProjectTestsTool("", authdomain.NewAutoApproveAuthorizer(t.TempDir()))), ToolCall: &call}
	t.Run("tui", func(t *testing.T) {
		out := formatter.FormatEvent(event, 120)
		require.NotEmpty(t, out)
		assert.Equal(t, "• Run Tests ./...", stripANSI(out))
		assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorAccent, false, false)+" "))
		assert.Contains(t, out, ansiWrap("Run Tests", pal, colorColorful, false, true))
	})
	t.Run("cli", func(t *testing.T) {
		out := formatter.FormatEvent(event, MinTerminalWidth)
		require.NotEmpty(t, out)
		assert.Equal(t, "• Run Tests ./...", stripANSI(out))
	})
}
func TestRunProjectTestsCompleteSuccessShowsPassed(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	formatter := NewTUIFormatter(cfg)
	call := llmstream.ToolCall{Name: "run_project_tests", Input: `{}`}
	result := llmstream.ToolResult{Result: `{"success":true,"content":"(elided)"}`, IsError: false}
	event := agent.Event{Type: agent.EventTypeToolComplete, Tool: extToolWithPresenter(t, exttools.NewRunProjectTestsTool("", authdomain.NewAutoApproveAuthorizer(t.TempDir()))), ToolCall: &call, ToolResult: &result}
	out := formatter.FormatEvent(event, 120)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, []string{
		"• Ran Tests ./...",
		"  └ Passed",
	}, lines)
	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorGreen, false, false)+" "))
}
func TestRunProjectTestsCompleteFailureShowsPackages(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	formatter := NewTUIFormatter(cfg)
	call := llmstream.ToolCall{Name: "run_project_tests", Input: `{}`}
	result := llmstream.ToolResult{Result: `{"success":false,"content":"Failed:\nsome/pkg1\nother/pkg2\n"}`, IsError: false}
	event := agent.Event{Type: agent.EventTypeToolComplete, Tool: extToolWithPresenter(t, exttools.NewRunProjectTestsTool("", authdomain.NewAutoApproveAuthorizer(t.TempDir()))), ToolCall: &call, ToolResult: &result}
	t.Run("tui", func(t *testing.T) {
		out := formatter.FormatEvent(event, 120)
		require.NotEmpty(t, out)
		lines := strings.Split(stripANSI(out), "\n")
		require.Equal(t, []string{
			"• Ran Tests ./...",
			"  └ Failed:",
			"    some/pkg1",
			"    other/pkg2",
		}, lines)
		assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorRed, false, false)+" "))
	})
	t.Run("cli", func(t *testing.T) {
		out := formatter.FormatEvent(event, MinTerminalWidth)
		require.NotEmpty(t, out)
		lines := strings.Split(stripANSI(out), "\n")
		require.Equal(t, []string{
			"• Ran Tests ./...",
			"  └ Failed:",
			"    some/pkg1",
			"    other/pkg2",
		}, lines)
	})
}

func TestUserMessageQueuedFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	formatter := NewTUIFormatter(cfg)
	event := agent.Event{
		Type:        agent.EventTypeUserMessageQueued,
		UserMessage: "this is a message",
	}

	t.Run("tui", func(t *testing.T) {
		out := formatter.FormatEvent(event, 80)
		require.NotEmpty(t, out)
		require.Equal(t, " › this is a message (queued)", stripANSI(out))
		assert.True(t, strings.HasPrefix(out, " "+ansiWrap("›", pal, colorAccent, false, false)+" "))
	})

	t.Run("cli", func(t *testing.T) {
		out := formatter.FormatEvent(event, MinTerminalWidth)
		require.NotEmpty(t, out)
		require.Equal(t, " › this is a message (queued)", stripANSI(out))
	})
}

func TestUserMessageQueuedWrapIndent(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)
	event := agent.Event{
		Type:        agent.EventTypeUserMessageQueued,
		UserMessage: "one two three four five six seven eight nine ten",
	}
	out := formatter.FormatEvent(event, MinTerminalWidth+1)
	require.NotEmpty(t, out)

	lines := strings.Split(stripANSI(out), "\n")
	require.Greater(t, len(lines), 1)
	require.True(t, strings.HasPrefix(lines[0], " › "))
	for i := 1; i < len(lines); i++ {
		assert.True(t, strings.HasPrefix(lines[i], "   "))
	}
}

func TestQueuedUserMessageSentFormatting(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)
	queuedEvent := agent.Event{
		Type:        agent.EventTypeUserMessageQueued,
		UserMessage: "this is a message",
	}
	sentEvent := agent.Event{
		Type:        agent.EventTypeQueuedUserMessageSent,
		UserMessage: "this is a message",
	}
	assert.Equal(t, " › this is a message (queued)", stripANSI(formatter.FormatEvent(queuedEvent, 80)))
	assert.Equal(t, " › this is a message", stripANSI(formatter.FormatEvent(sentEvent, 80)))
	assert.Equal(t, " › this is a message (queued)", stripANSI(formatter.FormatEvent(queuedEvent, MinTerminalWidth)))
	assert.Equal(t, " › this is a message", stripANSI(formatter.FormatEvent(sentEvent, MinTerminalWidth)))
}

func TestSubAgentIndentationTUI(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	call := llmstream.ToolCall{
		Name:  "run_tests",
		Input: `{"path":"./codeai/gocodecontext"}`,
	}
	content := `<test-status ok="true">
$ go test ./codeai/gocodecontext
ok  	axi/codeai/gocodecontext	0.374s
</test-status>
<lint-status ok="true">
$ gofmt -l -w internal/agentformatter
</lint-status>`
	payload := map[string]any{"content": content}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	result := llmstream.ToolResult{
		Result:  string(data),
		IsError: false,
	}
	event := agent.Event{
		Agent:      agent.AgentMeta{Depth: 1},
		Type:       agent.EventTypeToolComplete,
		Tool:       extToolWithPresenter(t, exttools.NewRunTestsTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), nil)),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 80)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, 2, len(lines))
	assert.Equal(t, "  • Ran Tests ./codeai/gocodecontext", lines[0])
	assert.Equal(t, "    └ Tests: pass | Lints: pass", lines[1])
}

func TestSubAgentIndentationCLI(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	call := llmstream.ToolCall{
		Name:  "run_tests",
		Input: `{"path":"./codeai/gocodecontext"}`,
	}
	content := `<test-status ok="true">
$ go test ./codeai/gocodecontext
ok  	axi/codeai/gocodecontext	0.374s
</test-status>
<lint-status ok="true">
$ gofmt -l -w internal/agentformatter
</lint-status>`
	payload := map[string]any{"content": content}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	result := llmstream.ToolResult{
		Result:  string(data),
		IsError: false,
	}
	event := agent.Event{
		Agent:      agent.AgentMeta{Depth: 2},
		Type:       agent.EventTypeToolComplete,
		Tool:       extToolWithPresenter(t, exttools.NewRunTestsTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), nil)),
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, MinTerminalWidth)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, 2, len(lines))
	assert.Equal(t, "    • Ran Tests ./codeai/gocodecontext", lines[0])
	assert.Equal(t, "      └ Tests: pass | Lints: pass", lines[1])
}

func stripANSI(in string) string {
	var b strings.Builder
	for i := 0; i < len(in); {
		if in[i] == 0x1b {
			i++
			for i < len(in) && in[i] != 'm' {
				i++
			}
			if i < len(in) {
				i++
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(in[i:])
		if size <= 0 {
			size = 1
		}
		b.WriteRune(r)
		i += size
	}
	return b.String()
}
