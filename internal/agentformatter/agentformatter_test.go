package agentformatter

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/termformat"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var dedent = gocodetesting.Dedent

func ansiWrap(text string, pal palette, c colorRole, italics bool, bold bool) string {
	style := pal.style(runeStyle{
		color:  c,
		italic: italics,
		bold:   bold,
	})
	return style.Wrap(text)
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

func TestToolCallTableDriven(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	formatter := NewTUIFormatter(cfg)
	pal := newPalette(cfg)

	testCases := []struct {
		name     string
		call     llmstream.ToolCall
		tuiWidth int
		expected string
	}{
		{
			name: "ls",
			call: llmstream.ToolCall{
				Name:  "ls",
				Input: `{"path":"codeai"}`,
			},
			tuiWidth: 60,
			expected: "• " + ansiWrap("List", pal, colorColorful, false, true) + " codeai",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Convert only the bullet of the message to be ANSI escaped (so the test case looks nicer, without stuff like \x1b[38;5;153m in there).
			// Other things that need escaping will need to include it in the expected test case.
			expected := strings.Replace(tc.expected, "•", ansiWrap("•", pal, colorAccent, false, false), 1)

			event := agent.Event{
				Type:     agent.EventTypeToolCall,
				ToolCall: &tc.call,
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
		Tool:     "shell",
		ToolCall: &call,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 72)
	require.NotEmpty(t, out)

	assert.True(t, strings.HasPrefix(out, ansiWrap("•", pal, colorAccent, false, false)+" "), "bullet should use accent palette")
	runningSeq := ansiWrap("Running", pal, colorColorful, false, true)
	assert.Contains(t, out, runningSeq, "verb should be bold and colorful")
	assert.NotContains(t, out, ansiWrap("go test .", pal, colorNone, true, false), "command should not be italicized")
	assert.Contains(t, stripANSI(out), "Running go test .", "full command should be present")
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
		Tool:       "shell",
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 90)
	stripped := stripANSI(out)
	require.NotEmpty(t, stripped)

	linesOut := strings.Split(stripped, "\n")
	require.GreaterOrEqual(t, len(linesOut), 3)
	assert.Equal(t, "• Ran go test .", linesOut[0])
	assert.Equal(t, "  └ "+termformat.Sanitize(lines[0], 4), linesOut[1])
	assert.Contains(t, linesOut, "    … +2 lines")

	assert.Contains(t, out, ansiWrap("•", pal, colorGreen, false, false))
	assert.Contains(t, out, ansiWrap("Ran", pal, colorColorful, false, true))
	assert.NotContains(t, out, ansiWrap("go test .", pal, colorNone, true, false))
}

func TestToolCallReadFileVerbColor(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(240, 240, 240),
	}
	pal := newPalette(cfg)

	call := llmstream.ToolCall{
		Name:  "read_file",
		Input: `{"path":"codeai/tools/shell.go"}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     "read_file",
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

	call := llmstream.ToolCall{
		Name:  "read_file",
		Input: `{"path":"codeai/tools/shell.go"}`,
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
		Tool:       "read_file",
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

	call := llmstream.ToolCall{
		Name:  "read_file",
		Input: `{"path":"missing.txt"}`,
	}
	result := llmstream.ToolResult{
		Result:  "path does not exist",
		IsError: true,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       "read_file",
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

func TestLsCompleteSuccessNoOutput(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)

	call := llmstream.ToolCall{
		Name:  "ls",
		Input: `{"path":"."}`,
	}
	result := llmstream.ToolResult{
		Result:  `{"success":true,"content":"- file1\n- file2"}`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       "ls",
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

func TestLsCompleteErrorShowsMessage(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)

	call := llmstream.ToolCall{
		Name:  "ls",
		Input: `{"path":"/tmp/unknown"}`,
	}
	result := llmstream.ToolResult{
		Result:  "path does not exist",
		IsError: true,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       "ls",
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
		Tool:     "apply_patch",
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
		Tool:     "apply_patch",
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
		Tool:     "apply_patch",
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
		Tool:       "apply_patch",
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
		Tool:     "update_plan",
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
		Tool:       "update_plan",
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
		Tool:     "update_plan",
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
		Tool:     "update_usage",
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
		Tool:       "update_usage",
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
		Tool:       "update_usage",
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

func TestGetPublicAPICallWithIdentifiers(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	pal := newPalette(cfg)
	call := llmstream.ToolCall{
		Name:  "get_public_api",
		Input: `{"import_path":"axi/some/pkg","identifiers":["SomeType","DoThingFunc"]}`,
	}
	event := agent.Event{
		Type:     agent.EventTypeToolCall,
		Tool:     "get_public_api",
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
		Input: `{"import_path":"axi/another/pkg","identifiers":["T","F"]}`,
	}
	result := llmstream.ToolResult{
		Result:  `{"success":true}`,
		IsError: false,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       "get_public_api",
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
		Input: `{"import_path":"axi/bad/pkg","identifiers":["X"]}`,
	}
	result := llmstream.ToolResult{
		Result:  "package not found",
		IsError: true,
	}
	event := agent.Event{
		Type:       agent.EventTypeToolComplete,
		Tool:       "get_public_api",
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
		Tool:     "clarify_public_api",
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
		Tool:       "clarify_public_api",
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
		Tool:       "clarify_public_api",
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

func TestSubAgentIndentationTUI(t *testing.T) {
	cfg := Config{
		BackgroundColor: termformat.NewRGBColor(0, 0, 0),
		ForegroundColor: termformat.NewRGBColor(255, 255, 255),
	}
	call := llmstream.ToolCall{
		Name:  "run_tests",
		Input: `{"path":"./codeai/gocodecontext"}`,
	}
	content := "$ go test ./codeai/gocodecontext\nok  	axi/codeai/gocodecontext\t0.374s"
	payload := map[string]any{
		"success": true,
		"content": content,
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	result := llmstream.ToolResult{
		Result:  string(data),
		IsError: false,
	}
	event := agent.Event{
		Agent:      agent.AgentMeta{Depth: 1},
		Type:       agent.EventTypeToolComplete,
		Tool:       "run_tests",
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, 80)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, 3, len(lines))
	assert.Equal(t, "  • Ran Tests ./codeai/gocodecontext", lines[0])
	assert.Equal(t, "    └ $ go test ./codeai/gocodecontext", lines[1])
	assert.Equal(t, "      "+termformat.Sanitize("ok  	axi/codeai/gocodecontext\t0.374s", 4), lines[2])
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
	content := "$ go test ./codeai/gocodecontext\nok  	axi/codeai/gocodecontext\t0.374s"
	payload := map[string]any{
		"success": true,
		"content": content,
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	result := llmstream.ToolResult{
		Result:  string(data),
		IsError: false,
	}
	event := agent.Event{
		Agent:      agent.AgentMeta{Depth: 2},
		Type:       agent.EventTypeToolComplete,
		Tool:       "run_tests",
		ToolCall:   &call,
		ToolResult: &result,
	}

	out := NewTUIFormatter(cfg).FormatEvent(event, MinTerminalWidth)
	require.NotEmpty(t, out)
	lines := strings.Split(stripANSI(out), "\n")
	require.Equal(t, 3, len(lines))
	assert.Equal(t, "    • Ran Tests ./codeai/gocodecontext", lines[0])
	assert.Equal(t, "      └ $ go test ./codeai/gocodecontext", lines[1])
	assert.Equal(t, "        "+termformat.Sanitize("ok  	axi/codeai/gocodecontext\t0.374s", 4), lines[2])
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
