package agentformatter

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/termformat"
	"github.com/codalotl/codalotl/internal/q/uni"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	gmtext "github.com/yuin/goldmark/text"
)

const MinTerminalWidth = 30

const sanitizeTabWidth = 4

func sanitizeText(s string) string {
	if s == "" {
		return ""
	}
	return termformat.Sanitize(s, sanitizeTabWidth)
}

type Formatter interface {
	// FormatEvent returns the content to print in a chat window or stdout-based CLI.
	//
	// If terminalWidth > MinTerminalWidth, newlines will be inserted precisely so that nothing wraps. Otherwise, paragraphs will not contain newlines and the caller can wrap themselves or insert the text in a container
	// that can deal with long strings.
	FormatEvent(e agent.Event, terminalWidth int) string
}

// Config controls the terminal colorization options.
type Config struct {
	PlainText       bool
	BackgroundColor termformat.Color
	ForegroundColor termformat.Color
	AccentColor     termformat.Color
	ColorfulColor   termformat.Color
	SuccessColor    termformat.Color
	ErrorColor      termformat.Color
}

type textTUIFormatter struct {
	cfg     Config
	palette palette
	md      goldmark.Markdown
}

// NewTUIFormatter creates a new Formatter configured for chat/TUI rendering.
//
// If ForegroundColor/BackgroundColor in c aren't passed in, they're determined by sending OSC codes to the terminal. The terminal can't be in Alt mode at this time.
func NewTUIFormatter(c Config) Formatter {
	return &textTUIFormatter{
		cfg:     c,
		palette: newPalette(c),
		md:      goldmark.New(),
	}
}

func (f *textTUIFormatter) FormatEvent(e agent.Event, terminalWidth int) string {
	if terminalWidth <= 0 {
		terminalWidth = MinTerminalWidth
	}

	indentWidth := e.Agent.Depth * 2

	if terminalWidth <= MinTerminalWidth {
		out := f.formatCLI(e)
		if indentWidth > 0 && out != "" {
			return indentLines(out, indentWidth)
		}
		return out
	}

	contentWidth := terminalWidth
	if indentWidth > 0 {
		contentWidth = maxInt(1, terminalWidth-indentWidth)
	}

	out := f.formatTUI(e, contentWidth)
	if indentWidth > 0 && out != "" {
		return indentLines(out, indentWidth)
	}
	return out
}

func indentLines(content string, indentWidth int) string {
	if indentWidth <= 0 || content == "" {
		return content
	}
	indent := strings.Repeat(" ", indentWidth)
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = indent + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (f *textTUIFormatter) formatCLI(e agent.Event) string {
	switch e.Type {
	case agent.EventTypeAssistantText:
		return f.cliAssistantText(e.TextContent.Content)
	case agent.EventTypeAssistantReasoning:
		return f.cliAssistantReasoning(e.ReasoningContent.Content)
	case agent.EventTypeToolCall:
		return f.cliToolCall(e)
	case agent.EventTypeToolComplete:
		return f.cliToolComplete(e)
	case agent.EventTypeWarning:
		return f.cliStatusLine("Warning", e.Error, colorAccent)
	case agent.EventTypeRetry:
		return f.cliStatusLine("Retry", e.Error, colorColorful)
	case agent.EventTypeCanceled:
		return f.cliStatusLine("Canceled", e.Error, colorRed)
	case agent.EventTypeError:
		return f.cliStatusLine("Error", e.Error, colorRed)
	case agent.EventTypeDoneSuccess:
		return f.cliPlainLine(colorGreen, "Agent finished the turn.")
	case agent.EventTypeAssistantTurnComplete:
		return f.cliTurnComplete(e)
	default:
		return ""
	}
}

func (f *textTUIFormatter) formatTUI(e agent.Event, terminalWidth int) string {
	switch e.Type {
	case agent.EventTypeAssistantText:
		return f.tuiAssistantText(e.TextContent.Content, terminalWidth)
	case agent.EventTypeAssistantReasoning:
		return f.tuiAssistantReasoning(e.ReasoningContent.Content, terminalWidth)
	case agent.EventTypeToolCall:
		return f.tuiToolCall(e, terminalWidth)
	case agent.EventTypeToolComplete:
		return f.tuiToolComplete(e, terminalWidth)
	case agent.EventTypeWarning:
		return f.tuiStatusLine("Warning", e.Error, terminalWidth, colorAccent)
	case agent.EventTypeRetry:
		return f.tuiStatusLine("Retry", e.Error, terminalWidth, colorColorful)
	case agent.EventTypeCanceled:
		return f.tuiStatusLine("Canceled", e.Error, terminalWidth, colorRed)
	case agent.EventTypeError:
		return f.tuiStatusLine("Error", e.Error, terminalWidth, colorRed)
	case agent.EventTypeDoneSuccess:
		return f.tuiSimpleLine("Agent finished the turn.", terminalWidth, colorGreen, false)
	case agent.EventTypeAssistantTurnComplete:
		return f.tuiTurnComplete(e, terminalWidth)
	default:
		return ""
	}
}

func (f *textTUIFormatter) cliAssistantText(content string) string {
	content = sanitizeText(content)
	if strings.TrimSpace(content) == "" {
		return ""
	}
	runes := f.buildStyledRunes(content, runeStyle{color: colorNormal}, f.codeRanges(content))
	return f.cliSimpleLine(runes, colorAccent)
}

func (f *textTUIFormatter) tuiAssistantText(content string, width int) string {
	content = sanitizeText(content)
	if strings.TrimSpace(content) == "" {
		return ""
	}
	runes := f.buildStyledRunes(content, runeStyle{color: colorNormal}, f.codeRanges(content))
	return f.wrapStyledText(runes, width, f.bulletPrefix(colorAccent), "  ")
}

var reasoningSummaryPattern = regexp.MustCompile(`(?s)^\s*\*\*(.+?)\*\*\s*(?:\n+(.*))?$`)
var usageResultPattern = regexp.MustCompile(`^\d+:`)

func (f *textTUIFormatter) tuiAssistantReasoning(content string, width int) string {
	content = sanitizeText(content)
	summary, ok := extractReasoningSummary(content)
	if ok {
		runes := f.buildStyledRunes(summary, runeStyle{color: colorNormal, italic: true}, nil)
		return f.wrapStyledText(runes, width, f.bulletPrefix(colorAccent), "  ")
	}

	runes := f.buildStyledRunes(content, runeStyle{color: colorNormal, italic: true}, nil)
	return f.wrapStyledText(runes, width, f.bulletPrefix(colorAccent), "  ")
}

func (f *textTUIFormatter) cliAssistantReasoning(content string) string {
	content = sanitizeText(content)
	summary, ok := extractReasoningSummary(content)
	if ok {
		runes := f.buildStyledRunes(summary, runeStyle{color: colorNormal, italic: true}, nil)
		return f.cliSimpleLine(runes, colorAccent)
	}
	runes := f.buildStyledRunes(content, runeStyle{color: colorNormal, italic: true}, nil)
	return f.cliSimpleLine(runes, colorAccent)
}

func extractReasoningSummary(content string) (string, bool) {
	matches := reasoningSummaryPattern.FindStringSubmatch(content)
	if len(matches) < 2 {
		return "", false
	}
	summary := strings.TrimSpace(matches[1])
	if summary == "" {
		return "", false
	}
	return sanitizeText(summary), true
}

func normalizedToolName(e agent.Event) string {
	if e.Tool != "" {
		return strings.ToLower(e.Tool)
	}
	if e.ToolCall != nil {
		if e.ToolCall.Name != "" {
			return strings.ToLower(e.ToolCall.Name)
		}
		if e.ToolCall.Type != "" {
			return strings.ToLower(e.ToolCall.Type)
		}
	}
	return ""
}

func toolDisplayName(e agent.Event) string {
	var name string
	if e.Tool != "" {
		name = e.Tool
	} else if e.ToolCall != nil {
		if e.ToolCall.Name != "" {
			name = e.ToolCall.Name
		} else if e.ToolCall.Type != "" {
			name = e.ToolCall.Type
		}
	}
	if name == "" {
		return "tool call"
	}
	return sanitizeText(name)
}

func (f *textTUIFormatter) tuiToolCall(e agent.Event, width int) string {
	switch normalizedToolName(e) {
	case "shell":
		return f.tuiShellToolCall(e, width)
	case "ls":
		return f.tuiLsToolCall(e, width)
	case "read_file":
		return f.tuiReadFileToolCall(e, width)
	case "diagnostics":
		return f.tuiDiagnosticsToolCall(e, width)
	case "get_public_api":
		return f.tuiGetPublicAPIToolCall(e, width)
	case "clarify_public_api":
		return f.tuiClarifyPublicAPIToolCall(e, width)
	case "get_usage":
		return f.tuiGetUsageToolCall(e, width)
	case "module_info":
		return f.tuiModuleInfoToolCall(e, width)
	case "run_tests":
		return f.tuiRunTestsToolCall(e, width)
	case "run_project_tests":
		return f.tuiRunProjectTestsToolCall(e, width)
	case "apply_patch":
		return f.tuiApplyPatchToolCall(e, width)
	case "update_plan":
		return f.tuiUpdatePlanToolCall(e, width)
	case "update_usage":
		return f.tuiUpdateUsageToolCall(e, width)
	case "change_api":
		return f.tuiChangeAPIToolCall(e, width)
	default:
		return f.tuiGenericToolCall(e, width)
	}
}

func (f *textTUIFormatter) cliToolCall(e agent.Event) string {
	switch normalizedToolName(e) {
	case "shell":
		return f.cliShellToolCall(e)
	case "ls":
		return f.cliLsToolCall(e)
	case "read_file":
		return f.cliReadFileToolCall(e)
	case "diagnostics":
		return f.cliDiagnosticsToolCall(e)
	case "get_public_api":
		return f.cliGetPublicAPIToolCall(e)
	case "clarify_public_api":
		return f.cliClarifyPublicAPIToolCall(e)
	case "get_usage":
		return f.cliGetUsageToolCall(e)
	case "module_info":
		return f.cliModuleInfoToolCall(e)
	case "run_tests":
		return f.cliRunTestsToolCall(e)
	case "run_project_tests":
		return f.cliRunProjectTestsToolCall(e)
	case "apply_patch":
		return f.cliApplyPatchToolCall(e)
	case "update_plan":
		return f.cliUpdatePlanToolCall(e)
	case "update_usage":
		return f.cliUpdateUsageToolCall(e)
	case "change_api":
		return f.cliChangeAPIToolCall(e)
	default:
		return f.cliGenericToolCall(e)
	}
}

func (f *textTUIFormatter) tuiShellToolCall(e agent.Event, width int) string {
	command, ok := extractShellCommand(e.ToolCall)
	target := strings.TrimSpace(command)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Running", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	return f.tuiBulletLine(width, colorAccent, segments...)
}

func (f *textTUIFormatter) cliShellToolCall(e agent.Event) string {
	command, ok := extractShellCommand(e.ToolCall)
	target := strings.TrimSpace(command)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Running", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	return f.cliBulletLine(colorAccent, segments...)
}

func (f *textTUIFormatter) tuiLsToolCall(e agent.Event, width int) string {
	path, ok := extractLsPath(e.ToolCall)
	target := strings.TrimSpace(path)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "List", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	return f.tuiBulletLine(width, colorAccent, segments...)
}

func (f *textTUIFormatter) cliLsToolCall(e agent.Event) string {
	path, ok := extractLsPath(e.ToolCall)
	target := strings.TrimSpace(path)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "List", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	return f.cliBulletLine(colorAccent, segments...)
}

func (f *textTUIFormatter) tuiReadFileToolCall(e agent.Event, width int) string {
	path, ok := extractReadFilePath(e.ToolCall)
	target := strings.TrimSpace(path)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Read", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	return f.tuiBulletLine(width, colorAccent, segments...)
}

func (f *textTUIFormatter) cliReadFileToolCall(e agent.Event) string {
	path, ok := extractReadFilePath(e.ToolCall)
	target := strings.TrimSpace(path)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Read", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	return f.cliBulletLine(colorAccent, segments...)
}

func (f *textTUIFormatter) tuiGenericToolCall(e agent.Event, width int) string {
	name := toolDisplayName(e)
	segments := []textSegment{
		{text: "Tool", style: runeStyle{color: colorColorful, bold: true}},
	}
	if name != "" {
		segments = append(segments, textSegment{text: " " + name})
	}
	if e.ToolCall != nil {
		if input := strings.TrimSpace(sanitizeText(e.ToolCall.Input)); input != "" {
			segments = append(segments, textSegment{text: " " + input})
		}
	}
	return f.tuiBulletLine(width, colorAccent, segments...)
}

func (f *textTUIFormatter) cliGenericToolCall(e agent.Event) string {
	name := toolDisplayName(e)
	segments := []textSegment{
		{text: "Tool", style: runeStyle{color: colorColorful, bold: true}},
	}
	if name != "" {
		segments = append(segments, textSegment{text: " " + name})
	}
	if e.ToolCall != nil {
		if input := strings.TrimSpace(sanitizeText(e.ToolCall.Input)); input != "" {
			segments = append(segments, textSegment{text: " " + input})
		}
	}
	return f.cliBulletLine(colorAccent, segments...)
}

func (f *textTUIFormatter) tuiToolComplete(e agent.Event, width int) string {
	success, cmd, outputLines := f.parseToolResult(e)
	switch normalizedToolName(e) {
	case "apply_patch":
		return f.tuiApplyPatchToolComplete(e, width, success, cmd, outputLines)
	case "shell":
		return f.tuiShellToolComplete(e, width, success, cmd, outputLines)
	case "ls":
		return f.tuiLsToolComplete(e, width, success, cmd, outputLines)
	case "read_file":
		return f.tuiReadFileToolComplete(e, width, success, cmd, outputLines)
	case "diagnostics":
		return f.tuiDiagnosticsToolComplete(e, width, success)
	case "get_public_api":
		return f.tuiGetPublicAPIToolComplete(e, width, success, cmd, outputLines)
	case "clarify_public_api":
		return f.tuiClarifyPublicAPIToolComplete(e, width, success, cmd, outputLines)
	case "get_usage":
		return f.tuiGetUsageToolComplete(e, width, success, cmd, outputLines)
	case "module_info":
		return f.tuiModuleInfoToolComplete(e, width, success, cmd, outputLines)
	case "run_tests":
		return f.tuiRunTestsToolComplete(e, width, success, cmd, outputLines)
	case "run_project_tests":
		return f.tuiRunProjectTestsToolComplete(e, width, success, cmd, outputLines)
	case "update_plan":
		return f.tuiUpdatePlanToolComplete(e, width, success, cmd, outputLines)
	case "update_usage":
		return f.tuiUpdateUsageToolComplete(e, width, success, cmd, outputLines)
	case "change_api":
		return f.tuiChangeAPIToolComplete(e, width, success, cmd, outputLines)
	default:
		return f.tuiGenericToolComplete(e, width, success, cmd, outputLines)
	}
}

func (f *textTUIFormatter) cliToolComplete(e agent.Event) string {
	success, cmd, outputLines := f.parseToolResult(e)
	switch normalizedToolName(e) {
	case "apply_patch":
		return f.cliApplyPatchToolComplete(e, success, cmd, outputLines)
	case "shell":
		return f.cliShellToolComplete(e, success, cmd, outputLines)
	case "ls":
		return f.cliLsToolComplete(e, success, cmd, outputLines)
	case "read_file":
		return f.cliReadFileToolComplete(e, success, cmd, outputLines)
	case "diagnostics":
		return f.cliDiagnosticsToolComplete(e, success)
	case "get_public_api":
		return f.cliGetPublicAPIToolComplete(e, success, cmd, outputLines)
	case "clarify_public_api":
		return f.cliClarifyPublicAPIToolComplete(e, success, cmd, outputLines)
	case "get_usage":
		return f.cliGetUsageToolComplete(e, success, cmd, outputLines)
	case "module_info":
		return f.cliModuleInfoToolComplete(e, success, cmd, outputLines)
	case "run_tests":
		return f.cliRunTestsToolComplete(e, success, cmd, outputLines)
	case "run_project_tests":
		return f.cliRunProjectTestsToolComplete(e, success, cmd, outputLines)
	case "update_plan":
		return f.cliUpdatePlanToolComplete(e, success, cmd, outputLines)
	case "update_usage":
		return f.cliUpdateUsageToolComplete(e, success, cmd, outputLines)
	case "change_api":
		return f.cliChangeAPIToolComplete(e, success, cmd, outputLines)
	default:
		return f.cliGenericToolComplete(e, success, cmd, outputLines)
	}
}

func (f *textTUIFormatter) toolOutputFirstPrefix() string {
	var builder strings.Builder
	builder.WriteString("  ")
	f.appendStyled(&builder, f.buildStyledRunes("└", runeStyle{color: colorAccent}, nil))
	builder.WriteString(" ")
	return builder.String()
}

func (f *textTUIFormatter) cliToolOutputPrefix(first bool) []styledRune {
	if first {
		var runes []styledRune
		runes = append(runes, f.buildStyledRunes("  ", runeStyle{color: colorNormal}, nil)...)
		runes = append(runes, f.buildStyledRunes("└", runeStyle{color: colorAccent}, nil)...)
		runes = append(runes, f.buildStyledRunes(" ", runeStyle{color: colorNormal}, nil)...)
		return runes
	}
	return f.buildStyledRunes("    ", runeStyle{color: colorNormal}, nil)
}

func (f *textTUIFormatter) appendTUIToolOutput(builder *strings.Builder, width int, lines []toolOutputLine) {
	if len(lines) == 0 {
		return
	}

	for idx, line := range lines {
		text := sanitizeText(line.text)
		builder.WriteByte('\n')
		prefix := "    "
		if idx == 0 {
			prefix = f.toolOutputFirstPrefix()
		}
		var ranges []byteRange
		if line.highlightCode {
			ranges = f.codeRanges(text)
		}
		runes := f.buildStyledRunes(text, line.style, ranges)
		builder.WriteString(f.wrapStyledText(runes, width, prefix, "    "))
	}
}

func (f *textTUIFormatter) cliToolOutputLines(lines []toolOutputLine) []string {
	if len(lines) == 0 {
		return nil
	}
	result := make([]string, 0, len(lines))
	for idx, line := range lines {
		text := sanitizeText(line.text)
		var runes []styledRune
		runes = append(runes, f.cliToolOutputPrefix(idx == 0)...)
		var ranges []byteRange
		if line.highlightCode {
			ranges = f.codeRanges(text)
		}
		runes = append(runes, f.buildStyledRunes(text, line.style, ranges)...)
		result = append(result, f.styledString(runes))
	}
	return result
}

func (f *textTUIFormatter) tuiShellToolComplete(e agent.Event, width int, success bool, cmd string, outputLines []toolOutputLine) string {
	target := strings.TrimSpace(cmd)
	if target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Ran", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, bullet, segments...))
	f.appendTUIToolOutput(&builder, width, outputLines)
	return builder.String()
}

func (f *textTUIFormatter) cliShellToolComplete(e agent.Event, success bool, cmd string, outputLines []toolOutputLine) string {
	target := strings.TrimSpace(cmd)
	if target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Ran", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	lines := []string{f.cliBulletLine(bullet, segments...)}
	if rest := f.cliToolOutputLines(outputLines); len(rest) > 0 {
		lines = append(lines, rest...)
	}
	return strings.Join(lines, "\n")
}

func (f *textTUIFormatter) tuiLsToolComplete(e agent.Event, width int, success bool, _ string, outputLines []toolOutputLine) string {
	path, ok := extractLsPath(e.ToolCall)
	target := strings.TrimSpace(path)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "List", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, bullet, segments...))
	if !success && len(outputLines) > 0 {
		f.appendTUIToolOutput(&builder, width, outputLines)
	}
	return builder.String()
}

func (f *textTUIFormatter) cliLsToolComplete(e agent.Event, success bool, _ string, outputLines []toolOutputLine) string {
	path, ok := extractLsPath(e.ToolCall)
	target := strings.TrimSpace(path)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "List", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	lines := []string{f.cliBulletLine(bullet, segments...)}
	if !success {
		if rest := f.cliToolOutputLines(outputLines); len(rest) > 0 {
			lines = append(lines, rest...)
		}
	}
	return strings.Join(lines, "\n")
}

func (f *textTUIFormatter) tuiReadFileToolComplete(e agent.Event, width int, success bool, _ string, outputLines []toolOutputLine) string {
	path, ok := extractReadFilePath(e.ToolCall)
	target := strings.TrimSpace(path)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Read", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, bullet, segments...))
	if !success && len(outputLines) > 0 {
		f.appendTUIToolOutput(&builder, width, outputLines)
	}
	return builder.String()
}

func (f *textTUIFormatter) cliReadFileToolComplete(e agent.Event, success bool, _ string, outputLines []toolOutputLine) string {
	path, ok := extractReadFilePath(e.ToolCall)
	target := strings.TrimSpace(path)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Read", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	lines := []string{f.cliBulletLine(bullet, segments...)}
	if !success {
		if rest := f.cliToolOutputLines(outputLines); len(rest) > 0 {
			lines = append(lines, rest...)
		}
	}
	return strings.Join(lines, "\n")
}

func (f *textTUIFormatter) tuiGenericToolComplete(e agent.Event, width int, success bool, _ string, outputLines []toolOutputLine) string {
	name := toolDisplayName(e)
	segments := []textSegment{
		{text: "Tool", style: runeStyle{color: colorColorful, bold: true}},
	}
	if name != "" {
		segments = append(segments, textSegment{text: " " + name})
	}
	if e.ToolCall != nil {
		if input := strings.TrimSpace(sanitizeText(e.ToolCall.Input)); input != "" {
			segments = append(segments, textSegment{text: " " + input})
		}
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, bullet, segments...))
	f.appendTUIToolOutput(&builder, width, outputLines)
	return builder.String()
}

func (f *textTUIFormatter) cliGenericToolComplete(e agent.Event, success bool, _ string, outputLines []toolOutputLine) string {
	name := toolDisplayName(e)
	segments := []textSegment{
		{text: "Tool", style: runeStyle{color: colorColorful, bold: true}},
	}
	if name != "" {
		segments = append(segments, textSegment{text: " " + name})
	}
	if e.ToolCall != nil {
		if input := strings.TrimSpace(sanitizeText(e.ToolCall.Input)); input != "" {
			segments = append(segments, textSegment{text: " " + input})
		}
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	lines := []string{f.cliBulletLine(bullet, segments...)}
	if rest := f.cliToolOutputLines(outputLines); len(rest) > 0 {
		lines = append(lines, rest...)
	}
	return strings.Join(lines, "\n")
}

func (f *textTUIFormatter) tuiStatusLine(kind string, err error, width int, c colorRole) string {
	msg := kind
	if err != nil {
		msg = fmt.Sprintf("%s: %s", kind, err)
	}
	msg = sanitizeText(msg)
	runes := f.buildStyledRunes(msg, runeStyle{color: colorNormal}, nil)
	return f.wrapStyledText(runes, width, f.bulletPrefix(c), "  ")
}

func (f *textTUIFormatter) cliStatusLine(kind string, err error, c colorRole) string {
	msg := kind
	if err != nil {
		msg = fmt.Sprintf("%s: %s", kind, err)
	}
	msg = sanitizeText(msg)
	runes := f.buildStyledRunes(msg, runeStyle{color: colorNormal}, nil)
	return f.cliSimpleLine(runes, c)
}

func (f *textTUIFormatter) tuiSimpleLine(message string, width int, c colorRole, italic bool) string {
	message = sanitizeText(message)
	runes := f.buildStyledRunes(message, runeStyle{color: colorNormal, italic: italic}, nil)
	return f.wrapStyledText(runes, width, f.bulletPrefix(c), "  ")
}

func (f *textTUIFormatter) cliPlainLine(c colorRole, message string) string {
	message = sanitizeText(message)
	runes := f.buildStyledRunes(message, runeStyle{color: colorNormal}, nil)
	return f.cliSimpleLine(runes, c)
}

func (f *textTUIFormatter) cliSimpleLine(runes []styledRune, c colorRole) string {
	builder := strings.Builder{}
	builder.WriteString(f.bulletPrefix(c))
	f.appendStyled(&builder, runes)
	return builder.String()
}

func (f *textTUIFormatter) tuiTurnComplete(e agent.Event, width int) string {
	if e.Turn == nil {
		return ""
	}
	usage := e.Turn.Usage
	text := fmt.Sprintf("Turn complete: finish=%s input=%d output=%d reasoning=%d cached_input=%d",
		e.Turn.FinishReason,
		usage.TotalInputTokens,
		usage.TotalOutputTokens,
		usage.ReasoningTokens,
		usage.CachedInputTokens,
	)
	text = sanitizeText(text)
	runes := f.buildStyledRunes(text, runeStyle{color: colorNormal}, nil)
	return f.wrapStyledText(runes, width, f.bulletPrefix(colorAccent), "  ")
}

func (f *textTUIFormatter) cliTurnComplete(e agent.Event) string {
	if e.Turn == nil {
		return ""
	}
	usage := e.Turn.Usage
	text := fmt.Sprintf("Turn complete: finish=%s input=%d output=%d reasoning=%d cached_input=%d",
		e.Turn.FinishReason,
		usage.TotalInputTokens,
		usage.TotalOutputTokens,
		usage.ReasoningTokens,
		usage.CachedInputTokens,
	)
	text = sanitizeText(text)
	runes := f.buildStyledRunes(text, runeStyle{color: colorNormal}, nil)
	return f.cliSimpleLine(runes, colorAccent)
}

type toolOutputLine struct {
	text          string
	style         runeStyle
	highlightCode bool
}

// parseToolResult returns success, command summary, and formatted output lines (limited to five entries).
func (f *textTUIFormatter) parseToolResult(e agent.Event) (bool, string, []toolOutputLine) {
	success := true
	if e.ToolResult != nil {
		success = !e.ToolResult.IsError
	}

	cmd, _ := extractShellCommand(e.ToolCall)

	var lines []toolOutputLine
	if e.ToolResult != nil {
		lines = summarizeToolResult(*e.ToolResult)
		if resultSuccess, ok := toolResultSuccess(*e.ToolResult); ok {
			success = resultSuccess
		}
	}
	return success, cmd, lines
}

func toolResultSuccess(result llmstream.ToolResult) (bool, bool) {
	trimmed := strings.TrimSpace(result.Result)

	// Prefer an explicit JSON `success` field when present.
	var payload struct {
		Success *bool  `json:"success"`
		Content string `json:"content"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		if payload.Success != nil {
			return *payload.Success, true
		}
		// Some tools report failures via an `error` string but keep IsError=false.
		if strings.TrimSpace(payload.Error) != "" {
			return false, true
		}
		// Some tools return XML-ish content like:
		//   <diagnostics-status ok="false">...</diagnostics-status>
		// If present, honor the ok="..." attribute for status/bullet coloring.
		if ok, found := extractXMLishOK(payload.Content); found {
			return ok, true
		}
		return false, false
	}

	// If the tool result isn't JSON, try to infer success from an outer XML-ish tag.
	if ok, found := extractXMLishOK(trimmed); found {
		return ok, true
	}

	return false, false
}

func extractXMLishOK(s string) (ok bool, found bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "<") {
		return false, false
	}
	gt := strings.IndexByte(s, '>')
	if gt <= 0 {
		return false, false
	}
	openTag := s[:gt]
	idx := strings.Index(openTag, "ok=")
	if idx < 0 {
		return false, false
	}
	rest := openTag[idx+len("ok="):]
	if len(rest) < 3 {
		return false, false
	}
	quote := rest[0]
	if quote != '"' && quote != '\'' {
		return false, false
	}
	closing := strings.IndexByte(rest[1:], quote)
	if closing < 0 {
		return false, false
	}
	val := rest[1 : 1+closing]
	if strings.EqualFold(val, "true") {
		return true, true
	}
	if strings.EqualFold(val, "false") {
		return false, true
	}
	return false, false
}

func summarizeToolResult(result llmstream.ToolResult) []toolOutputLine {
	trimmed := strings.TrimSpace(result.Result)
	if trimmed == "" {
		return nil
	}

	var payload struct {
		Content string `json:"content"`
		Error   string `json:"error"`
		Success *bool  `json:"success"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		if payload.Error != "" {
			return []toolOutputLine{{
				text:          sanitizeText(fmt.Sprintf("Error: %s", payload.Error)),
				style:         runeStyle{color: colorRed},
				highlightCode: false,
			}}
		}
		if payload.Content != "" {
			return summarizeToolContent(payload.Content)
		}
	}

	if result.IsError {
		return []toolOutputLine{{
			text:          sanitizeText(fmt.Sprintf("Error: %s", trimmed)),
			style:         runeStyle{color: colorRed},
			highlightCode: false,
		}}
	}

	return summarizeToolContent(trimmed)
}

func summarizeToolContent(content string) []toolOutputLine {
	content = sanitizeText(content)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")

	start := 0
	for i, line := range lines {
		if strings.TrimSpace(line) == "Output:" {
			start = i + 1
			break
		}
	}

	lines = lines[start:]
	lines = trimEmpty(lines)
	if len(lines) == 0 {
		return nil
	}

	const maxLines = 5
	var summarised []string
	if len(lines) > maxLines {
		remaining := len(lines) - maxLines
		summarised = append(lines[:maxLines], fmt.Sprintf("… +%d lines", remaining))
	} else {
		summarised = lines
	}

	output := make([]toolOutputLine, 0, len(summarised))
	for idx, line := range summarised {
		highlight := true
		if idx == len(summarised)-1 && strings.HasPrefix(line, "… +") {
			highlight = false
		}
		output = append(output, toolOutputLine{
			text:          sanitizeText(line),
			style:         runeStyle{color: colorAccent},
			highlightCode: highlight,
		})
	}
	return output
}

func trimEmpty(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func extractShellCommand(call *llmstream.ToolCall) (string, bool) {
	if call == nil {
		return "", false
	}
	if strings.ToLower(call.Name) != "shell" && !strings.EqualFold(call.Type, "shell") {
		return "", false
	}

	var payload struct {
		Command []string `json:"command"`
	}
	if err := json.Unmarshal([]byte(call.Input), &payload); err != nil {
		return "", false
	}
	if len(payload.Command) == 0 {
		return "", false
	}
	return sanitizeText(strings.Join(payload.Command, " ")), true
}

func extractLsPath(call *llmstream.ToolCall) (string, bool) {
	if call == nil {
		return "", false
	}
	var payload struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(call.Input), &payload); err != nil {
		return "", false
	}
	path := strings.TrimSpace(payload.Path)
	if path == "" {
		return "", false
	}
	return sanitizeText(path), true
}

func extractReadFilePath(call *llmstream.ToolCall) (string, bool) {
	if call == nil {
		return "", false
	}
	var payload struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(call.Input), &payload); err != nil {
		return "", false
	}
	path := strings.TrimSpace(payload.Path)
	if path == "" {
		return "", false
	}
	return sanitizeText(path), true
}

func extractDiagnosticsPath(call *llmstream.ToolCall) (string, bool) {
	if call == nil {
		return "", false
	}
	var payload struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(call.Input), &payload); err != nil {
		return "", false
	}
	path := strings.TrimSpace(payload.Path)
	if path == "" {
		return "", false
	}
	return sanitizeText(path), true
}

func (f *textTUIFormatter) tuiDiagnosticsToolCall(e agent.Event, width int) string {
	path, ok := extractDiagnosticsPath(e.ToolCall)
	target := strings.TrimSpace(path)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Run Diagnostics", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	return f.tuiBulletLine(width, colorAccent, segments...)
}

func (f *textTUIFormatter) cliDiagnosticsToolCall(e agent.Event) string {
	path, ok := extractDiagnosticsPath(e.ToolCall)
	target := strings.TrimSpace(path)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Run Diagnostics", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	return f.cliBulletLine(colorAccent, segments...)
}

func (f *textTUIFormatter) tuiDiagnosticsToolComplete(e agent.Event, width int, success bool) string {
	path, ok := extractDiagnosticsPath(e.ToolCall)
	target := strings.TrimSpace(path)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := []textSegment{
		{text: "Ran Diagnostics", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	// Per SPEC, diagnostics never prints output lines; status is indicated by bullet color.
	return f.tuiBulletLine(width, bullet, segments...)
}

func (f *textTUIFormatter) cliDiagnosticsToolComplete(e agent.Event, success bool) string {
	path, ok := extractDiagnosticsPath(e.ToolCall)
	target := strings.TrimSpace(path)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := []textSegment{
		{text: "Ran Diagnostics", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	// Per SPEC, diagnostics never prints output lines; status is indicated by bullet color.
	return f.cliBulletLine(bullet, segments...)
}

func (f *textTUIFormatter) tuiGetPublicAPIToolCall(e agent.Event, width int) string {
	path, identifiers, ok := extractGetPublicAPI(e.ToolCall)
	target := strings.TrimSpace(path)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Read Public API", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, colorAccent, segments...))
	if len(identifiers) > 0 {
		line := toolOutputLine{
			text:          strings.Join(identifiers, ", "),
			style:         runeStyle{color: colorAccent},
			highlightCode: true,
		}
		f.appendTUIToolOutput(&builder, width, []toolOutputLine{line})
	}
	return builder.String()
}

func (f *textTUIFormatter) cliGetPublicAPIToolCall(e agent.Event) string {
	path, identifiers, ok := extractGetPublicAPI(e.ToolCall)
	target := strings.TrimSpace(path)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Read Public API", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	lines := []string{f.cliBulletLine(colorAccent, segments...)}
	if len(identifiers) > 0 {
		line := toolOutputLine{
			text:          strings.Join(identifiers, ", "),
			style:         runeStyle{color: colorAccent},
			highlightCode: true,
		}
		lines = append(lines, f.cliToolOutputLines([]toolOutputLine{line})...)
	}
	return strings.Join(lines, "\n")
}

func (f *textTUIFormatter) tuiClarifyPublicAPIToolCall(e agent.Event, width int) string {
	identifier, path, question, ok := extractClarifyPublicAPI(e.ToolCall)
	if !ok {
		return f.tuiGenericToolCall(e, width)
	}
	segments := []textSegment{
		{text: "Clarifying API", style: runeStyle{color: colorColorful, bold: true}},
	}
	if identifier != "" {
		segments = append(segments, textSegment{text: " " + identifier})
	}
	if path != "" {
		segments = append(segments, textSegment{text: " in", style: runeStyle{color: colorAccent}})
		segments = append(segments, textSegment{text: " " + path})
	}
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, colorAccent, segments...))
	question = strings.TrimSpace(question)
	if question != "" {
		line := toolOutputLine{
			text:          question,
			style:         runeStyle{color: colorAccent},
			highlightCode: true,
		}
		f.appendTUIToolOutput(&builder, width, []toolOutputLine{line})
	}
	return builder.String()
}

func (f *textTUIFormatter) cliClarifyPublicAPIToolCall(e agent.Event) string {
	identifier, path, question, ok := extractClarifyPublicAPI(e.ToolCall)
	if !ok {
		return f.cliGenericToolCall(e)
	}
	segments := []textSegment{
		{text: "Clarifying API", style: runeStyle{color: colorColorful, bold: true}},
	}
	if identifier != "" {
		segments = append(segments, textSegment{text: " " + identifier})
	}
	if path != "" {
		segments = append(segments, textSegment{text: " in", style: runeStyle{color: colorAccent}})
		segments = append(segments, textSegment{text: " " + path})
	}
	lines := []string{f.cliBulletLine(colorAccent, segments...)}
	question = strings.TrimSpace(question)
	if question != "" {
		line := toolOutputLine{
			text:          question,
			style:         runeStyle{color: colorAccent},
			highlightCode: true,
		}
		lines = append(lines, f.cliToolOutputLines([]toolOutputLine{line})...)
	}
	return strings.Join(lines, "\n")
}

func (f *textTUIFormatter) tuiGetPublicAPIToolComplete(e agent.Event, width int, success bool, _ string, outputLines []toolOutputLine) string {
	path, identifiers, ok := extractGetPublicAPI(e.ToolCall)
	target := strings.TrimSpace(path)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := []textSegment{
		{text: "Read Public API", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, bullet, segments...))
	if success {
		if len(identifiers) > 0 {
			f.appendTUIToolOutput(&builder, width, []toolOutputLine{{
				text:          strings.Join(identifiers, ", "),
				style:         runeStyle{color: colorAccent},
				highlightCode: true,
			}})
		}
	} else if len(outputLines) > 0 {
		f.appendTUIToolOutput(&builder, width, outputLines)
	}
	return builder.String()
}

func (f *textTUIFormatter) cliGetPublicAPIToolComplete(e agent.Event, success bool, _ string, outputLines []toolOutputLine) string {
	path, identifiers, ok := extractGetPublicAPI(e.ToolCall)
	target := strings.TrimSpace(path)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := []textSegment{
		{text: "Read Public API", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	lines := []string{f.cliBulletLine(bullet, segments...)}
	if success {
		if len(identifiers) > 0 {
			lines = append(lines, f.cliToolOutputLines([]toolOutputLine{{
				text:          strings.Join(identifiers, ", "),
				style:         runeStyle{color: colorAccent},
				highlightCode: true,
			}})...)
		}
	} else if rest := f.cliToolOutputLines(outputLines); len(rest) > 0 {
		lines = append(lines, rest...)
	}
	return strings.Join(lines, "\n")
}

func (f *textTUIFormatter) tuiClarifyPublicAPIToolComplete(e agent.Event, width int, success bool, _ string, outputLines []toolOutputLine) string {
	identifier, path, _, ok := extractClarifyPublicAPI(e.ToolCall)
	if !ok {
		return f.tuiGenericToolComplete(e, width, success, "", outputLines)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := []textSegment{
		{text: "Clarified API", style: runeStyle{color: colorColorful, bold: true}},
	}
	if identifier != "" {
		segments = append(segments, textSegment{text: " " + identifier})
	}
	if path != "" {
		segments = append(segments, textSegment{text: " in", style: runeStyle{color: colorAccent}})
		segments = append(segments, textSegment{text: " " + path})
	}
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, bullet, segments...))
	if len(outputLines) > 0 {
		f.appendTUIToolOutput(&builder, width, outputLines)
	}
	return builder.String()
}

func (f *textTUIFormatter) cliClarifyPublicAPIToolComplete(e agent.Event, success bool, _ string, outputLines []toolOutputLine) string {
	identifier, path, _, ok := extractClarifyPublicAPI(e.ToolCall)
	if !ok {
		return f.cliGenericToolComplete(e, success, "", outputLines)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := []textSegment{
		{text: "Clarified API", style: runeStyle{color: colorColorful, bold: true}},
	}
	if identifier != "" {
		segments = append(segments, textSegment{text: " " + identifier})
	}
	if path != "" {
		segments = append(segments, textSegment{text: " in", style: runeStyle{color: colorAccent}})
		segments = append(segments, textSegment{text: " " + path})
	}
	lines := []string{f.cliBulletLine(bullet, segments...)}
	if rest := f.cliToolOutputLines(outputLines); len(rest) > 0 {
		lines = append(lines, rest...)
	}
	return strings.Join(lines, "\n")
}

func (f *textTUIFormatter) tuiGetUsageToolCall(e agent.Event, width int) string {
	pkg, identifier, ok := extractGetUsage(e.ToolCall)
	target := strings.TrimSpace(pkg)
	id := strings.TrimSpace(identifier)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Read Usage", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	if id != "" {
		segments = append(segments, textSegment{text: " " + id})
	}
	return f.tuiBulletLine(width, colorAccent, segments...)
}

func (f *textTUIFormatter) cliGetUsageToolCall(e agent.Event) string {
	pkg, identifier, ok := extractGetUsage(e.ToolCall)
	target := strings.TrimSpace(pkg)
	id := strings.TrimSpace(identifier)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Read Usage", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	if id != "" {
		segments = append(segments, textSegment{text: " " + id})
	}
	return f.cliBulletLine(colorAccent, segments...)
}

// -------- module_info formatting --------

func extractModuleInfo(call *llmstream.ToolCall) (packageSearch string, includeDeps bool, ok bool) {
	if call == nil {
		return "", false, false
	}
	var payload struct {
		PackageSearch         string `json:"package_search"`
		IncludeDependencyPkgs bool   `json:"include_dependency_packages"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(call.Input)), &payload); err != nil {
		// No options is a valid/expected case for module_info, so failure to parse isn't fatal.
		return "", false, false
	}
	packageSearch = strings.TrimSpace(payload.PackageSearch)
	includeDeps = payload.IncludeDependencyPkgs
	return sanitizeText(packageSearch), includeDeps, true
}

func moduleInfoOptionsLine(call *llmstream.ToolCall) (string, bool) {
	search, deps, ok := extractModuleInfo(call)
	_ = ok // parsing is best-effort; absence/invalid JSON means no options line.

	var parts []string
	if strings.TrimSpace(search) != "" {
		parts = append(parts, "Search: "+strings.TrimSpace(search))
	}
	if deps {
		parts = append(parts, "Deps: true")
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "; "), true
}

func (f *textTUIFormatter) tuiModuleInfoToolCall(e agent.Event, width int) string {
	segments := []textSegment{
		{text: "Read Module Info", style: runeStyle{color: colorColorful, bold: true}},
	}
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, colorAccent, segments...))
	if options, ok := moduleInfoOptionsLine(e.ToolCall); ok {
		f.appendTUIToolOutput(&builder, width, []toolOutputLine{{
			text:          options,
			style:         runeStyle{color: colorAccent},
			highlightCode: false,
		}})
	}
	return builder.String()
}

func (f *textTUIFormatter) cliModuleInfoToolCall(e agent.Event) string {
	segments := []textSegment{
		{text: "Read Module Info", style: runeStyle{color: colorColorful, bold: true}},
	}
	lines := []string{f.cliBulletLine(colorAccent, segments...)}
	if options, ok := moduleInfoOptionsLine(e.ToolCall); ok {
		lines = append(lines, f.cliToolOutputLines([]toolOutputLine{{
			text:          options,
			style:         runeStyle{color: colorAccent},
			highlightCode: false,
		}})...)
	}
	return strings.Join(lines, "\n")
}

func (f *textTUIFormatter) tuiModuleInfoToolComplete(e agent.Event, width int, success bool, _ string, _ []toolOutputLine) string {
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := []textSegment{
		{text: "Read Module Info", style: runeStyle{color: colorColorful, bold: true}},
	}
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, bullet, segments...))
	// Per SPEC, completion mirrors the call (except status), and does not print the tool output.
	if options, ok := moduleInfoOptionsLine(e.ToolCall); ok {
		f.appendTUIToolOutput(&builder, width, []toolOutputLine{{
			text:          options,
			style:         runeStyle{color: colorAccent},
			highlightCode: false,
		}})
	}
	return builder.String()
}

func (f *textTUIFormatter) cliModuleInfoToolComplete(e agent.Event, success bool, _ string, _ []toolOutputLine) string {
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := []textSegment{
		{text: "Read Module Info", style: runeStyle{color: colorColorful, bold: true}},
	}
	lines := []string{f.cliBulletLine(bullet, segments...)}
	// Per SPEC, completion mirrors the call (except status), and does not print the tool output.
	if options, ok := moduleInfoOptionsLine(e.ToolCall); ok {
		lines = append(lines, f.cliToolOutputLines([]toolOutputLine{{
			text:          options,
			style:         runeStyle{color: colorAccent},
			highlightCode: false,
		}})...)
	}
	return strings.Join(lines, "\n")
}

func (f *textTUIFormatter) tuiGetUsageToolComplete(e agent.Event, width int, success bool, _ string, outputLines []toolOutputLine) string {
	pkg, identifier, ok := extractGetUsage(e.ToolCall)
	target := strings.TrimSpace(pkg)
	id := strings.TrimSpace(identifier)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Read Usage", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	if id != "" {
		segments = append(segments, textSegment{text: " " + id})
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, bullet, segments...))
	if success {
		count := usageResultCount(e.ToolResult)
		noun := "results"
		if count == 1 {
			noun = "result"
		}
		f.appendTUIToolOutput(&builder, width, []toolOutputLine{{
			text:          fmt.Sprintf("Found %d %s.", count, noun),
			style:         runeStyle{color: colorAccent},
			highlightCode: false,
		}})
	} else if len(outputLines) > 0 {
		f.appendTUIToolOutput(&builder, width, outputLines)
	}
	return builder.String()
}

func (f *textTUIFormatter) cliGetUsageToolComplete(e agent.Event, success bool, _ string, outputLines []toolOutputLine) string {
	pkg, identifier, ok := extractGetUsage(e.ToolCall)
	target := strings.TrimSpace(pkg)
	id := strings.TrimSpace(identifier)
	if !ok || target == "" {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Read Usage", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	if id != "" {
		segments = append(segments, textSegment{text: " " + id})
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	lines := []string{f.cliBulletLine(bullet, segments...)}
	if success {
		count := usageResultCount(e.ToolResult)
		noun := "results"
		if count == 1 {
			noun = "result"
		}
		lines = append(lines, f.cliToolOutputLines([]toolOutputLine{{
			text:          fmt.Sprintf("Found %d %s.", count, noun),
			style:         runeStyle{color: colorAccent},
			highlightCode: false,
		}})...)
	} else if rest := f.cliToolOutputLines(outputLines); len(rest) > 0 {
		lines = append(lines, rest...)
	}
	return strings.Join(lines, "\n")
}

func usageResultCount(result *llmstream.ToolResult) int {
	if result == nil {
		return 0
	}
	content := usageResultContent(*result)
	if content == "" {
		return 0
	}
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	count := 0
	for _, line := range lines {
		if usageResultPattern.MatchString(line) {
			count++
		}
	}
	return count
}

func usageResultContent(result llmstream.ToolResult) string {
	trimmed := strings.TrimSpace(result.Result)
	if trimmed == "" {
		return ""
	}
	var payload struct {
		Content string `json:"content"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		if payload.Error != "" {
			return ""
		}
		if payload.Content != "" {
			return sanitizeText(strings.TrimSpace(payload.Content))
		}
	}
	return sanitizeText(trimmed)
}

// -------- run_tests formatting --------

// extractRunTests parses the run_tests tool input.
// Expected JSON shape (fields are optional):
//
//	{"path":"./pkg","test_name":"SomeTest","verbose":true}
func extractRunTests(call *llmstream.ToolCall) (path string, testName string, verbose bool, ok bool) {
	if call == nil {
		return "", "", false, false
	}
	// Allow either Name or Type to identify the tool, but don't require it.
	if !strings.EqualFold(call.Name, "run_tests") && !strings.EqualFold(call.Type, "run_tests") {
		// fallthrough; still try to parse
	}
	var payload struct {
		Path     string `json:"path"`
		TestName string `json:"test_name"`
		Verbose  bool   `json:"verbose"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(call.Input)), &payload); err != nil {
		return "", "", false, false
	}
	rawPath := strings.TrimSpace(payload.Path)
	rawTestName := strings.TrimSpace(payload.TestName)
	sanitizedTestName := sanitizeText(rawTestName)
	if rawPath == "" {
		return "", sanitizedTestName, payload.Verbose, false
	}
	return sanitizeText(rawPath), sanitizedTestName, payload.Verbose, true
}

func buildRunTestsHeader(path, testName string, verbose bool) string {
	// Per SPEC, the header after "Run Tests"/"Ran Tests" should only show the path.
	// Any flags (e.g., -v, -run) are already echoed in the tool output (`$ go test ...`).
	return strings.TrimSpace(path)
}

func (f *textTUIFormatter) tuiRunTestsToolCall(e agent.Event, width int) string {
	path, testName, verbose, ok := extractRunTests(e.ToolCall)
	target := ""
	if ok {
		// The path after Run Tests ... is just the 'path' param printed as-is, plus any flags in header.
		target = buildRunTestsHeader(path, testName, verbose)
	} else {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Run Tests", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	return f.tuiBulletLine(width, colorAccent, segments...)
}

func (f *textTUIFormatter) cliRunTestsToolCall(e agent.Event) string {
	path, testName, verbose, ok := extractRunTests(e.ToolCall)
	target := ""
	if ok {
		target = buildRunTestsHeader(path, testName, verbose)
	} else {
		target = toolDisplayName(e)
	}
	segments := []textSegment{
		{text: "Run Tests", style: runeStyle{color: colorColorful, bold: true}},
	}
	if target != "" {
		segments = append(segments, textSegment{text: " " + target})
	}
	return f.cliBulletLine(colorAccent, segments...)
}

// stripOuterXMLTag removes a single outermost XML-like tag, returning its inner text if present.
func stripOuterXMLTag(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 3 || s[0] != '<' {
		return s
	}
	gt := strings.IndexByte(s, '>')
	if gt <= 1 {
		return s
	}
	// Extract tag name up to whitespace or '>'
	tagPart := s[1:gt]
	for i, r := range tagPart {
		if unicode.IsSpace(r) {
			tagPart = tagPart[:i]
			break
		}
	}
	if tagPart == "" {
		return s
	}
	closeTag := "</" + tagPart + ">"
	if !strings.HasSuffix(s, closeTag) {
		return s
	}
	inner := s[gt+1 : len(s)-len(closeTag)]
	return strings.TrimSpace(inner)
}

// summarizeRunTests builds output lines for run_tests, including the echoed `$ go test <path>` and stripping any outer XML tag.
func (f *textTUIFormatter) summarizeRunTests(e agent.Event, path string) (success bool, lines []toolOutputLine) {
	success = true
	var content string
	if e.ToolResult != nil {
		// Determine success from structured payload if available.
		if s, ok := toolResultSuccess(*e.ToolResult); ok {
			success = s
		} else {
			success = !e.ToolResult.IsError
		}
		trimmed := strings.TrimSpace(e.ToolResult.Result)
		var payload struct {
			Content string `json:"content"`
			Error   string `json:"error"`
			Success *bool  `json:"success"`
		}
		if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
			if payload.Error != "" {
				return success, []toolOutputLine{{
					text:          fmt.Sprintf("Error: %s", payload.Error),
					style:         runeStyle{color: colorRed},
					highlightCode: false,
				}}
			}
			content = payload.Content
		} else {
			if e.ToolResult.IsError {
				return success, []toolOutputLine{{
					text:          "Error: " + trimmed,
					style:         runeStyle{color: colorRed},
					highlightCode: false,
				}}
			}
			content = trimmed
		}
	}
	content = stripOuterXMLTag(strings.TrimSpace(content))

	// Do not reconstruct the command; it's already included in the tool output.
	// Just summarize the provided content (already stripped of any outer XML).
	lines = summarizeToolContent(content)
	return success, lines
}

func (f *textTUIFormatter) tuiRunTestsToolComplete(e agent.Event, width int, _ bool, _ string, _ []toolOutputLine) string {
	path, testName, verbose, ok := extractRunTests(e.ToolCall)
	var header string
	if ok {
		header = buildRunTestsHeader(path, testName, verbose)
	} else {
		header = toolDisplayName(e)
	}
	success, lines := f.summarizeRunTests(e, path)
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := []textSegment{
		{text: "Ran Tests", style: runeStyle{color: colorColorful, bold: true}},
	}
	if header != "" {
		segments = append(segments, textSegment{text: " " + header})
	}
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, bullet, segments...))
	f.appendTUIToolOutput(&builder, width, lines)
	return builder.String()
}

func (f *textTUIFormatter) cliRunTestsToolComplete(e agent.Event, _ bool, _ string, _ []toolOutputLine) string {
	path, testName, verbose, ok := extractRunTests(e.ToolCall)
	var header string
	if ok {
		header = buildRunTestsHeader(path, testName, verbose)
	} else {
		header = toolDisplayName(e)
	}
	success, lines := f.summarizeRunTests(e, path)
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := []textSegment{
		{text: "Ran Tests", style: runeStyle{color: colorColorful, bold: true}},
	}
	if header != "" {
		segments = append(segments, textSegment{text: " " + header})
	}
	out := []string{f.cliBulletLine(bullet, segments...)}
	if rest := f.cliToolOutputLines(lines); len(rest) > 0 {
		out = append(out, rest...)
	}
	return strings.Join(out, "\n")
}

// -------- run_project_tests formatting --------

// For run_project_tests there is no meaningful input to show; headers should not include a path.
func (f *textTUIFormatter) tuiRunProjectTestsToolCall(e agent.Event, width int) string {
	segments := []textSegment{
		{text: "Run Project Tests", style: runeStyle{color: colorColorful, bold: true}},
	}
	return f.tuiBulletLine(width, colorAccent, segments...)
}

func (f *textTUIFormatter) cliRunProjectTestsToolCall(e agent.Event) string {
	segments := []textSegment{
		{text: "Run Project Tests", style: runeStyle{color: colorColorful, bold: true}},
	}
	return f.cliBulletLine(colorAccent, segments...)
}

func (f *textTUIFormatter) tuiRunProjectTestsToolComplete(e agent.Event, width int, _ bool, _ string, _ []toolOutputLine) string {
	// Reuse the same output summarization as run_tests.
	success, lines := f.summarizeRunTests(e, "")
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := []textSegment{
		{text: "Ran Project Tests", style: runeStyle{color: colorColorful, bold: true}},
	}
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, bullet, segments...))
	f.appendTUIToolOutput(&builder, width, lines)
	return builder.String()
}

func (f *textTUIFormatter) cliRunProjectTestsToolComplete(e agent.Event, _ bool, _ string, _ []toolOutputLine) string {
	success, lines := f.summarizeRunTests(e, "")
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := []textSegment{
		{text: "Ran Project Tests", style: runeStyle{color: colorColorful, bold: true}},
	}
	out := []string{f.cliBulletLine(bullet, segments...)}
	if rest := f.cliToolOutputLines(lines); len(rest) > 0 {
		out = append(out, rest...)
	}
	return strings.Join(out, "\n")
}

// extractGetPublicAPI extracts the path (which may be either a relative dir or an import path)
// and optional identifiers for get_public_api.
func extractGetPublicAPI(call *llmstream.ToolCall) (string, []string, bool) {
	if call == nil {
		return "", nil, false
	}
	// Allow either name or type to indicate the tool.
	if !strings.EqualFold(call.Name, "get_public_api") && !strings.EqualFold(call.Type, "get_public_api") {
		// We still try to parse, since some callers may omit Name/Type in tests;
		// but we won't reject based solely on tool name.
	}
	var payload struct {
		Path        string   `json:"path"`
		Identifiers []string `json:"identifiers"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(call.Input)), &payload); err != nil {
		return "", nil, false
	}
	path := strings.TrimSpace(payload.Path)
	if path == "" {
		return "", nil, false
	}
	// Clean identifiers: trim and drop empties.
	ids := make([]string, 0, len(payload.Identifiers))
	for _, id := range payload.Identifiers {
		id = strings.TrimSpace(id)
		if id != "" {
			ids = append(ids, sanitizeText(id))
		}
	}
	return sanitizeText(path), ids, true
}

func extractClarifyPublicAPI(call *llmstream.ToolCall) (identifier string, path string, question string, ok bool) {
	if call == nil {
		return "", "", "", false
	}
	var payload struct {
		Identifier string `json:"identifier"`
		Path       string `json:"path"`
		Question   string `json:"question"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(call.Input)), &payload); err != nil {
		return "", "", "", false
	}
	identifier = strings.TrimSpace(payload.Identifier)
	path = strings.TrimSpace(payload.Path)
	question = strings.TrimSpace(payload.Question)
	if identifier == "" && path == "" {
		return "", "", "", false
	}
	return sanitizeText(identifier), sanitizeText(path), sanitizeText(question), true
}

func extractGetUsage(call *llmstream.ToolCall) (string, string, bool) {
	if call == nil {
		return "", "", false
	}
	var payload struct {
		DefiningPackage string `json:"defining_package"`
		Package         string `json:"package"`
		ImportPath      string `json:"import_path"`
		Identifier      string `json:"identifier"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(call.Input)), &payload); err != nil {
		return "", "", false
	}
	pkg := strings.TrimSpace(payload.DefiningPackage)
	if pkg == "" {
		pkg = strings.TrimSpace(payload.Package)
	}
	if pkg == "" {
		pkg = strings.TrimSpace(payload.ImportPath)
	}
	id := strings.TrimSpace(payload.Identifier)
	if pkg == "" {
		return "", "", false
	}
	return sanitizeText(pkg), sanitizeText(id), true
}

func extractUpdateUsage(call *llmstream.ToolCall) (instructions string, paths []string, ok bool) {
	if call == nil {
		return "", nil, false
	}
	var payload struct {
		Instructions string   `json:"instructions"`
		Paths        []string `json:"paths"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(call.Input)), &payload); err != nil {
		return "", nil, false
	}
	rawInstructions := strings.TrimSpace(payload.Instructions)
	for _, p := range payload.Paths {
		p = strings.TrimSpace(p)
		if p != "" {
			paths = append(paths, sanitizeText(p))
		}
	}
	instructions = sanitizeText(rawInstructions)
	if len(paths) == 0 && instructions == "" {
		return "", nil, false
	}
	return instructions, paths, true
}

func extractChangeAPI(call *llmstream.ToolCall) (importPath string, instructions string, ok bool) {
	if call == nil {
		return "", "", false
	}
	var payload struct {
		ImportPath   string `json:"import_path"`
		Instructions string `json:"instructions"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(call.Input)), &payload); err != nil {
		return "", "", false
	}
	importPath = sanitizeText(strings.TrimSpace(payload.ImportPath))
	instructions = sanitizeText(strings.TrimSpace(payload.Instructions))
	if importPath == "" {
		return "", "", false
	}
	return importPath, instructions, true
}

func summarizeUpdateUsagePaths(paths []string) (summary string, extra int) {
	if len(paths) == 0 {
		return "", 0
	}
	limit := len(paths)
	if limit > 3 {
		limit = 3
	}
	summary = strings.Join(paths[:limit], ", ")
	extra = len(paths) - limit
	return summary, extra
}

func (f *textTUIFormatter) updateUsageHeaderSegments(verb string, paths []string) []textSegment {
	segments := []textSegment{
		{text: verb, style: runeStyle{color: colorColorful, bold: true}},
	}
	if len(paths) == 0 {
		return segments
	}
	summary, extra := summarizeUpdateUsagePaths(paths)
	if summary != "" {
		segments = append(segments, textSegment{text: " in", style: runeStyle{color: colorAccent}})
		segments = append(segments, textSegment{text: " " + summary})
	}
	if extra > 0 {
		segments = append(segments, textSegment{text: fmt.Sprintf(" (%d more)", extra), style: runeStyle{color: colorAccent}})
	}
	return segments
}

func (f *textTUIFormatter) tuiUpdateUsageToolCall(e agent.Event, width int) string {
	instructions, paths, ok := extractUpdateUsage(e.ToolCall)
	if !ok {
		return f.tuiGenericToolCall(e, width)
	}
	segments := f.updateUsageHeaderSegments("Updating Usage", paths)
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, colorAccent, segments...))
	instructions = strings.TrimSpace(instructions)
	if instructions != "" {
		line := toolOutputLine{
			text:          instructions,
			style:         runeStyle{color: colorAccent},
			highlightCode: true,
		}
		f.appendTUIToolOutput(&builder, width, []toolOutputLine{line})
	}
	return builder.String()
}

func (f *textTUIFormatter) cliUpdateUsageToolCall(e agent.Event) string {
	instructions, paths, ok := extractUpdateUsage(e.ToolCall)
	if !ok {
		return f.cliGenericToolCall(e)
	}
	segments := f.updateUsageHeaderSegments("Updating Usage", paths)
	lines := []string{f.cliBulletLine(colorAccent, segments...)}
	instructions = strings.TrimSpace(instructions)
	if instructions != "" {
		line := toolOutputLine{
			text:          instructions,
			style:         runeStyle{color: colorAccent},
			highlightCode: true,
		}
		lines = append(lines, f.cliToolOutputLines([]toolOutputLine{line})...)
	}
	return strings.Join(lines, "\n")
}

func (f *textTUIFormatter) tuiUpdateUsageToolComplete(e agent.Event, width int, success bool, _ string, outputLines []toolOutputLine) string {
	_, paths, ok := extractUpdateUsage(e.ToolCall)
	if !ok {
		return f.tuiGenericToolComplete(e, width, success, "", outputLines)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := f.updateUsageHeaderSegments("Updated Usage", paths)
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, bullet, segments...))
	if !success && len(outputLines) > 0 {
		f.appendTUIToolOutput(&builder, width, outputLines)
	}
	return builder.String()
}

func (f *textTUIFormatter) cliUpdateUsageToolComplete(e agent.Event, success bool, _ string, outputLines []toolOutputLine) string {
	_, paths, ok := extractUpdateUsage(e.ToolCall)
	if !ok {
		return f.cliGenericToolComplete(e, success, "", outputLines)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := f.updateUsageHeaderSegments("Updated Usage", paths)
	lines := []string{f.cliBulletLine(bullet, segments...)}
	if !success {
		if rest := f.cliToolOutputLines(outputLines); len(rest) > 0 {
			lines = append(lines, rest...)
		}
	}
	return strings.Join(lines, "\n")
}

func (f *textTUIFormatter) changeAPIHeaderSegments(verb string, importPath string) []textSegment {
	segments := []textSegment{
		{text: verb, style: runeStyle{color: colorColorful, bold: true}},
	}
	importPath = strings.TrimSpace(importPath)
	if importPath == "" {
		return segments
	}
	segments = append(segments, textSegment{text: " in", style: runeStyle{color: colorAccent}})
	segments = append(segments, textSegment{text: " " + importPath})
	return segments
}

func (f *textTUIFormatter) tuiChangeAPIToolCall(e agent.Event, width int) string {
	importPath, instructions, ok := extractChangeAPI(e.ToolCall)
	if !ok {
		return f.tuiGenericToolCall(e, width)
	}
	segments := f.changeAPIHeaderSegments("Changing API", importPath)
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, colorAccent, segments...))
	instructions = strings.TrimSpace(instructions)
	if instructions != "" {
		f.appendTUIToolOutput(&builder, width, []toolOutputLine{{
			text:          instructions,
			style:         runeStyle{color: colorAccent},
			highlightCode: true,
		}})
	}
	return builder.String()
}

func (f *textTUIFormatter) cliChangeAPIToolCall(e agent.Event) string {
	importPath, instructions, ok := extractChangeAPI(e.ToolCall)
	if !ok {
		return f.cliGenericToolCall(e)
	}
	segments := f.changeAPIHeaderSegments("Changing API", importPath)
	lines := []string{f.cliBulletLine(colorAccent, segments...)}
	instructions = strings.TrimSpace(instructions)
	if instructions != "" {
		lines = append(lines, f.cliToolOutputLines([]toolOutputLine{{
			text:          instructions,
			style:         runeStyle{color: colorAccent},
			highlightCode: true,
		}})...)
	}
	return strings.Join(lines, "\n")
}

func (f *textTUIFormatter) tuiChangeAPIToolComplete(e agent.Event, width int, success bool, _ string, outputLines []toolOutputLine) string {
	importPath, _, ok := extractChangeAPI(e.ToolCall)
	if !ok {
		return f.tuiGenericToolComplete(e, width, success, "", outputLines)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := f.changeAPIHeaderSegments("Changed API", importPath)
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, bullet, segments...))
	if !success && len(outputLines) > 0 {
		f.appendTUIToolOutput(&builder, width, outputLines)
	}
	return builder.String()
}

func (f *textTUIFormatter) cliChangeAPIToolComplete(e agent.Event, success bool, _ string, outputLines []toolOutputLine) string {
	importPath, _, ok := extractChangeAPI(e.ToolCall)
	if !ok {
		return f.cliGenericToolComplete(e, success, "", outputLines)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := f.changeAPIHeaderSegments("Changed API", importPath)
	lines := []string{f.cliBulletLine(bullet, segments...)}
	if !success {
		if rest := f.cliToolOutputLines(outputLines); len(rest) > 0 {
			lines = append(lines, rest...)
		}
	}
	return strings.Join(lines, "\n")
}

// updatePlanItem mirrors the structure returned by the update_plan tool.
type updatePlanItem struct {
	Step   string `json:"step"`
	Status string `json:"status"` // "pending", "in_progress", "completed"
}

// extractUpdatePlan extracts the explanation and plan items from an update_plan ToolCall.
func extractUpdatePlan(call *llmstream.ToolCall) (string, []updatePlanItem, bool) {
	if call == nil {
		return "", nil, false
	}
	// Accept either direct JSON or other content; we only handle JSON shape here.
	var payload struct {
		Explanation string           `json:"explanation"`
		Plan        []updatePlanItem `json:"plan"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(call.Input)), &payload); err != nil {
		return "", nil, false
	}
	// It's ok if explanation is blank; as long as we have a plan.
	if len(payload.Plan) == 0 && strings.TrimSpace(payload.Explanation) == "" {
		return "", nil, false
	}
	return strings.TrimSpace(payload.Explanation), payload.Plan, true
}

// updatePlanLines converts explanation/plan into toolOutputLine rows suitable for TUI/CLI append functions.
func (f *textTUIFormatter) updatePlanLines(explanation string, plan []updatePlanItem) []toolOutputLine {
	lines := make([]toolOutputLine, 0, 1+len(plan))
	exp := strings.TrimSpace(explanation)
	if exp != "" {
		lines = append(lines, toolOutputLine{
			text:          exp,
			style:         runeStyle{color: colorAccent},
			highlightCode: true,
		})
	}
	// Identify first uncompleted index.
	firstUncompleted := -1
	for i, it := range plan {
		if strings.ToLower(strings.TrimSpace(it.Status)) != "completed" {
			firstUncompleted = i
			break
		}
	}
	for i, it := range plan {
		label := strings.TrimSpace(it.Step)
		if label == "" {
			continue
		}
		box := "□"
		status := strings.ToLower(strings.TrimSpace(it.Status))
		if status == "completed" {
			box = "✔"
		}
		text := box + " " + label
		// Default styling for plan items.
		style := runeStyle{color: colorAccent}
		// Highlight the next step (first uncompleted) using Colorful.
		if firstUncompleted == i {
			style.color = colorColorful
		}
		// If this step is explicitly in progress, make it bold and colorful.
		if status == "in_progress" {
			style.color = colorColorful
			style.bold = true
		}
		lines = append(lines, toolOutputLine{
			text:          text,
			style:         style,
			highlightCode: true,
		})
	}
	return lines
}

func (f *textTUIFormatter) tuiUpdatePlanToolCall(e agent.Event, width int) string {
	expl, plan, ok := extractUpdatePlan(e.ToolCall)
	if !ok {
		// Fallback
		return f.tuiGenericToolCall(e, width)
	}
	segments := []textSegment{
		{text: "Update Plan", style: runeStyle{color: colorColorful, bold: true}},
	}
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, colorAccent, segments...))
	lines := f.updatePlanLines(expl, plan)
	f.appendTUIToolOutput(&builder, width, lines)
	return builder.String()
}

func (f *textTUIFormatter) cliUpdatePlanToolCall(e agent.Event) string {
	expl, plan, ok := extractUpdatePlan(e.ToolCall)
	if !ok {
		return f.cliGenericToolCall(e)
	}
	segments := []textSegment{
		{text: "Update Plan", style: runeStyle{color: colorColorful, bold: true}},
	}
	lines := []string{f.cliBulletLine(colorAccent, segments...)}
	output := f.cliToolOutputLines(f.updatePlanLines(expl, plan))
	if len(output) > 0 {
		lines = append(lines, output...)
	}
	return strings.Join(lines, "\n")
}

func (f *textTUIFormatter) tuiUpdatePlanToolComplete(e agent.Event, width int, success bool, _ string, _ []toolOutputLine) string {
	expl, plan, ok := extractUpdatePlan(e.ToolCall)
	if !ok {
		return f.tuiGenericToolComplete(e, width, success, "", nil)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := []textSegment{
		{text: "Update Plan", style: runeStyle{color: colorColorful, bold: true}},
	}
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, bullet, segments...))
	lines := f.updatePlanLines(expl, plan)
	f.appendTUIToolOutput(&builder, width, lines)
	return builder.String()
}

func (f *textTUIFormatter) cliUpdatePlanToolComplete(e agent.Event, success bool, _ string, _ []toolOutputLine) string {
	expl, plan, ok := extractUpdatePlan(e.ToolCall)
	if !ok {
		return f.cliGenericToolComplete(e, success, "", nil)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	segments := []textSegment{
		{text: "Update Plan", style: runeStyle{color: colorColorful, bold: true}},
	}
	lines := []string{f.cliBulletLine(bullet, segments...)}
	output := f.cliToolOutputLines(f.updatePlanLines(expl, plan))
	if len(output) > 0 {
		lines = append(lines, output...)
	}
	return strings.Join(lines, "\n")
}

type byteRange struct {
	start int
	end   int
}

func (f *textTUIFormatter) codeRanges(content string) []byteRange {
	source := []byte(content)
	reader := gmtext.NewReader(source)
	doc := f.md.Parser().Parse(reader)

	var ranges []byteRange
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if code, ok := n.(*ast.CodeSpan); ok {
			for child := code.FirstChild(); child != nil; child = child.NextSibling() {
				textNode, ok := child.(*ast.Text)
				if !ok {
					continue
				}
				segment := textNode.Segment
				if segment.IsEmpty() {
					continue
				}
				start := segment.Start
				stop := segment.Stop
				if start < 0 {
					start = 0
				}
				if stop > len(source) {
					stop = len(source)
				}
				if start < stop {
					ranges = append(ranges, byteRange{start: start, end: stop})
				}
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return ranges
}

type colorRole int

const (
	colorNone colorRole = iota
	colorNormal
	colorAccent
	colorGreen
	colorRed
	colorColorful
)

type palette struct {
	styles       map[colorRole]termformat.Style
	allowEffects bool
}

func newPalette(cfg Config) palette {

	noColorPalette := palette{
		styles:       map[colorRole]termformat.Style{},
		allowEffects: true,
	}

	if cfg.PlainText {
		return palette{
			styles:       map[colorRole]termformat.Style{},
			allowEffects: false,
		}
	}

	profile, err := termformat.GetColorProfile()
	if err != nil || profile == termformat.ColorProfileUncolored {
		return noColorPalette
	}

	foreground := cfg.ForegroundColor
	background := cfg.BackgroundColor

	if foreground == nil || background == nil {
		defaultFG, defaultBG := termformat.DefaultFBBGColor()
		if foreground == nil {
			foreground = defaultFG
		}
		if background == nil {
			background = defaultBG
		}
	}
	if cfg.ForegroundColor == (termformat.NoColor{}) || cfg.BackgroundColor == (termformat.NoColor{}) {
		return noColorPalette
	}

	foreground = profile.Convert(foreground)
	background = profile.Convert(background)

	accent := cfg.AccentColor
	if accent == nil {
		accent = profile.Convert(blendTermColors(foreground, background, 0.6))
	} else {
		accent = profile.Convert(accent)
	}
	colorful := cfg.ColorfulColor
	if colorful == nil {
		colorful = profile.Convert(defaultColorfulColor(background))
	} else {
		colorful = profile.Convert(colorful)
	}
	green := cfg.SuccessColor
	if green == nil {
		green = termformat.NewRGBColor(46, 139, 87)
	}
	green = profile.Convert(green)
	red := cfg.ErrorColor
	if red == nil {
		red = termformat.NewRGBColor(220, 82, 82)
	}
	red = profile.Convert(red)

	return palette{
		styles: map[colorRole]termformat.Style{
			colorNormal:   {Foreground: foreground},
			colorAccent:   {Foreground: accent},
			colorGreen:    {Foreground: green},
			colorRed:      {Foreground: red},
			colorColorful: {Foreground: colorful},
		},
		allowEffects: true,
	}
}

func (p palette) style(rs runeStyle) termformat.Style {
	style := p.styles[rs.color]
	if !p.allowEffects {
		return style
	}
	if rs.italic {
		style.Italic = termformat.StyleSetOn
	}
	if rs.bold {
		style.Bold = termformat.StyleSetOn
	}
	return style
}

func blendTermColors(fg, bg termformat.Color, fgWeight float64) termformat.Color {
	if fgWeight < 0 {
		fgWeight = 0
	}
	if fgWeight > 1 {
		fgWeight = 1
	}
	bgWeight := 1 - fgWeight
	fr, fgVal, fb := fg.RGB8()
	br, bgVal, bb := bg.RGB8()
	return termformat.NewRGBColor(
		uint8(float64(fr)*fgWeight+float64(br)*bgWeight),
		uint8(float64(fgVal)*fgWeight+float64(bgVal)*bgWeight),
		uint8(float64(fb)*fgWeight+float64(bb)*bgWeight),
	)
}

func defaultColorfulColor(bg termformat.Color) termformat.Color {
	r, g, b := bg.RGB8()
	brightness := perceivedBrightness(r, g, b)
	if brightness >= 180 {
		return termformat.NewRGBColor(24, 128, 255)
	}
	return termformat.NewRGBColor(90, 180, 255)
}

func perceivedBrightness(r, g, b uint8) float64 {
	return 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
}

type runeStyle struct {
	color  colorRole
	italic bool
	bold   bool
}

type styledRune struct {
	r         rune
	byteStart int
	byteEnd   int
	style     runeStyle
}

type textSegment struct {
	text  string
	style runeStyle
}

func (f *textTUIFormatter) buildStyledRunes(content string, base runeStyle, accentRanges []byteRange) []styledRune {
	if content == "" {
		return nil
	}

	runes := make([]styledRune, 0, len(content))
	for i, r := range content {
		size := utf8.RuneLen(r)
		if size < 0 {
			size = 1
		}
		style := base
		runes = append(runes, styledRune{
			r:         r,
			byteStart: i,
			byteEnd:   i + size,
			style:     style,
		})
	}
	if len(accentRanges) == 0 {
		return runes
	}

	for idx := range runes {
		for _, rng := range accentRanges {
			if runes[idx].byteStart >= rng.start && runes[idx].byteEnd <= rng.end {
				runes[idx].style.color = colorAccent
				break
			}
		}
	}
	return stripColorizedBackticks(runes)
}

func (f *textTUIFormatter) runesFromSegments(segments ...textSegment) []styledRune {
	if len(segments) == 0 {
		return nil
	}
	var out []styledRune
	for _, seg := range segments {
		text := sanitizeText(seg.text)
		if text == "" {
			continue
		}
		style := seg.style
		if style.color == colorNone {
			style.color = colorNormal
		}
		out = append(out, f.buildStyledRunes(text, style, nil)...)
	}
	return out
}

func (f *textTUIFormatter) tuiBulletLine(width int, bulletColor colorRole, segments ...textSegment) string {
	runes := f.runesFromSegments(segments...)
	return f.wrapStyledText(runes, width, f.bulletPrefix(bulletColor), "  ")
}

func (f *textTUIFormatter) cliBulletLine(bulletColor colorRole, segments ...textSegment) string {
	runes := f.runesFromSegments(segments...)
	return f.cliSimpleLine(runes, bulletColor)
}

func (f *textTUIFormatter) bulletPrefix(role colorRole) string {
	bulletRune := []styledRune{{
		r:     '•',
		style: runeStyle{color: role},
	}}
	var builder strings.Builder
	f.appendStyled(&builder, bulletRune)
	builder.WriteString(" ")
	return builder.String()
}

func (f *textTUIFormatter) wrapStyledText(content []styledRune, width int, firstPrefix, restPrefix string) string {
	var lines []line
	baseFirstPrefix := firstPrefix
	baseRestPrefix := restPrefix
	currentPrefixBase := baseFirstPrefix
	continuationPadding := ""
	currentLimit := maxInt(1, width-visibleLen(currentPrefixBase+continuationPadding))
	var buffer []styledRune
	currentWidth := 0
	lastSpace := -1

	appendLine := func(prefix string, runes []styledRune) {
		lineCopy := append([]styledRune(nil), runes...)
		lines = append(lines, line{prefix: prefix, runes: lineCopy})
	}

	updateLimit := func() {
		currentLimit = maxInt(1, width-visibleLen(currentPrefixBase+continuationPadding))
	}

	emitLine := func(runes []styledRune) {
		prefix := currentPrefixBase + continuationPadding
		appendLine(prefix, runes)
		currentPrefixBase = baseRestPrefix
		updateLimit()
	}

	for _, sr := range content {
		if sr.r == '\r' {
			continue
		}
		if sr.r == '\n' {
			emitLine(buffer)
			buffer = nil
			currentWidth = 0
			lastSpace = -1
			continuationPadding = ""
			updateLimit()
			continue
		}

		buffer = append(buffer, sr)
		currentWidth += runeWidth(sr.r)
		if isSpace(sr.r) {
			lastSpace = len(buffer) - 1
		}

		if currentWidth > currentLimit && currentLimit > 0 {
			breakIndex := lastSpace
			if breakIndex < 0 {
				breakIndex = len(buffer) - 1
			}
			firstPart := trimTrailingSpaces(buffer[:breakIndex])
			emitLine(firstPart)
			if pad := continuationPaddingForLine(firstPart); pad != "" || continuationPadding == "" {
				continuationPadding = pad
			}
			updateLimit()

			remainder := append([]styledRune(nil), buffer[breakIndex:]...)
			remainder = trimLeadingSpaces(remainder)

			buffer = remainder
			currentWidth = runesWidth(buffer)
			lastSpace = findLastSpace(buffer)
		}
	}

	if len(buffer) > 0 {
		emitLine(buffer)
	}

	var builder strings.Builder
	for idx, ln := range lines {
		builder.WriteString(ln.prefix)
		f.appendStyled(&builder, ln.runes)
		if idx < len(lines)-1 {
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

type line struct {
	prefix string
	runes  []styledRune
}

func continuationPaddingForLine(line []styledRune) string {
	width := listContinuationIndent(line)
	if width <= 0 {
		return ""
	}
	return strings.Repeat(" ", width)
}

func listContinuationIndent(line []styledRune) int {
	i := 0
	for i < len(line) && line[i].r == ' ' {
		i++
	}
	start := i
	if i >= len(line) {
		return 0
	}

	switch line[i].r {
	case '-', '*', '+':
		i++
		if i < len(line) && line[i].r == ' ' {
			i++
			return i
		}
		return 0
	}

	if unicode.IsDigit(line[i].r) {
		for i < len(line) && unicode.IsDigit(line[i].r) {
			i++
		}
		if i < len(line) && (line[i].r == '.' || line[i].r == ')') {
			i++
			if i < len(line) && line[i].r == ' ' {
				i++
				return i
			}
		}
	}

	return start
}

func trimTrailingSpaces(in []styledRune) []styledRune {
	end := len(in)
	for end > 0 {
		r := in[end-1].r
		if !isSpace(r) {
			break
		}
		end--
	}
	return in[:end]
}

func trimLeadingSpaces(in []styledRune) []styledRune {
	start := 0
	for start < len(in) && isSpace(in[start].r) {
		start++
	}
	return in[start:]
}

func runesWidth(runes []styledRune) int {
	total := 0
	for _, sr := range runes {
		total += runeWidth(sr.r)
	}
	return total
}

func findLastSpace(runes []styledRune) int {
	for i := len(runes) - 1; i >= 0; i-- {
		if isSpace(runes[i].r) {
			return i
		}
	}
	return -1
}

func runeWidth(r rune) int {
	if r == '\t' {
		return sanitizeTabWidth
	}
	width := uni.RuneWidth(r, nil)
	if width < 0 {
		return 0
	}
	return width
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t'
}

func stripColorizedBackticks(runes []styledRune) []styledRune {
	if len(runes) == 0 {
		return runes
	}

	skip := make(map[int]struct{})

	for i := 0; i < len(runes); {
		if runes[i].style.color != colorAccent {
			i++
			continue
		}

		start := i
		for i < len(runes) && runes[i].style.color == colorAccent {
			i++
		}

		for j := start - 1; j >= 0 && runes[j].r == '`'; j-- {
			skip[j] = struct{}{}
		}
		for j := i; j < len(runes) && runes[j].r == '`'; j++ {
			skip[j] = struct{}{}
		}
	}

	if len(skip) == 0 {
		return runes
	}

	result := make([]styledRune, 0, len(runes)-len(skip))
	for idx, sr := range runes {
		if _, ok := skip[idx]; ok {
			continue
		}
		result = append(result, sr)
	}
	return result
}

func (f *textTUIFormatter) appendStyled(builder *strings.Builder, runes []styledRune) {
	if len(runes) == 0 {
		return
	}

	writeSegment := func(segment []styledRune, style runeStyle) {
		if len(segment) == 0 {
			return
		}
		var text strings.Builder
		for _, sr := range segment {
			text.WriteRune(sr.r)
		}
		builder.WriteString(f.palette.style(style).Wrap(text.String()))
	}

	start := 0
	current := runes[0].style
	for i := 1; i <= len(runes); i++ {
		if i == len(runes) || runes[i].style != current {
			writeSegment(runes[start:i], current)
			if i < len(runes) {
				start = i
				current = runes[i].style
			}
		}
	}
}

func (f *textTUIFormatter) styledString(runes []styledRune) string {
	if len(runes) == 0 {
		return ""
	}
	var builder strings.Builder
	f.appendStyled(&builder, runes)
	return builder.String()
}

func visibleLen(s string) int {
	return termformat.TextWidthWithANSICodes(s)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
