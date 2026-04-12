package agentformatter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/termformat"
	"github.com/codalotl/codalotl/internal/q/uni"
	"github.com/codalotl/codalotl/internal/tools/authdomain"

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
	// If terminalWidth > MinTerminalWidth, newlines will be inserted precisely so that nothing wraps. Otherwise, paragraphs will not contain newlines and the caller
	// can wrap themselves or insert the text in a container that can deal with long strings.
	FormatEvent(e agent.Event, terminalWidth int) string
}

// Config controls the terminal colorization options. We need to know the intended bg/fg, so we can create other colors that are consistent. For instance, if we
// want to colorize backtick-wrapped paths/identifiers/code differently, can modify ForegroundColor to be closer to BackgroundColor.
type Config struct {
	PlainText       bool             // true: disable colors and ANSI escape characters (bold, italics, etc).
	BackgroundColor termformat.Color // the terminal's background color. If nil, uses termformat.DefaultFBBGColor.
	ForegroundColor termformat.Color // the terminal's foreground color. If nil, uses termformat.DefaultFBBGColor.
	AccentColor     termformat.Color // If nil, derived from fg/bg and downsampled to the detected color profile.
	ColorfulColor   termformat.Color // If nil, derived from fg/bg and downsampled to the detected color profile.
	SuccessColor    termformat.Color // If nil, uses a default green suitable for terminals, downsampled to the detected color profile.
	ErrorColor      termformat.Color // If nil, uses a default red suitable for terminals, downsampled to the detected color profile.
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
	requestedWidth := terminalWidth
	if terminalWidth <= 0 {
		terminalWidth = MinTerminalWidth
	}

	indentWidth := e.Agent.Depth * 2

	if requestedWidth <= 0 || (terminalWidth <= MinTerminalWidth && !presenterWidthConstrainedTUI(e)) {
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

// Replace presenters describe fixed-width TUI output, so an explicit width still wins at the narrow boundary.
func presenterWidthConstrainedTUI(e agent.Event) bool {
	presentation, ok := presenterPresentation(e)
	if !ok {
		return false
	}
	if presentation.Behavior != llmstream.CompletionBehaviorReplace {
		return false
	}
	if presentation.NarrowBehavior == llmstream.PresentationNarrowBehaviorPreferCLI {
		return false
	}
	if _, ok := presentationDiffBody(presentation); ok {
		return false
	}
	return true
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
	case agent.EventTypeUserMessageQueued:
		return f.cliUserMessage(e.UserMessage, true)
	case agent.EventTypeQueuedUserMessageSent:
		return f.cliUserMessage(e.UserMessage, false)
	case agent.EventTypeAssistantText:
		if shouldSuppressAssistantText(e) {
			return ""
		}
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
	case agent.EventTypeUserMessageQueued:
		return f.tuiUserMessage(e.UserMessage, terminalWidth, true)
	case agent.EventTypeQueuedUserMessageSent:
		return f.tuiUserMessage(e.UserMessage, terminalWidth, false)
	case agent.EventTypeAssistantText:
		if shouldSuppressAssistantText(e) {
			return ""
		}
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

func shouldSuppressAssistantText(e agent.Event) bool {
	if e.Type != agent.EventTypeAssistantText || e.Agent.Depth <= 0 {
		return false
	}
	_, ok := parseReviewToolPayload(e.TextContent.Content)
	return ok
}

func (f *textTUIFormatter) cliUserMessage(message string, queued bool) string {
	message = sanitizeText(message)
	if strings.TrimSpace(message) == "" {
		return ""
	}
	if queued {
		message += " (queued)"
	}
	var builder strings.Builder
	builder.WriteString(f.userPrefix())
	f.appendStyled(&builder, f.buildStyledRunes(message, runeStyle{color: colorNormal}, nil))
	return builder.String()
}

func (f *textTUIFormatter) tuiUserMessage(message string, width int, queued bool) string {
	message = sanitizeText(message)
	if strings.TrimSpace(message) == "" {
		return ""
	}
	if queued {
		message += " (queued)"
	}
	runes := f.buildStyledRunes(message, runeStyle{color: colorNormal}, nil)
	return f.wrapStyledText(runes, width, f.userPrefix(), "   ")
}

func (f *textTUIFormatter) userPrefix() string {
	var builder strings.Builder
	builder.WriteString(" ")
	f.appendStyled(&builder, []styledRune{{
		r:     '›',
		style: runeStyle{color: colorAccent},
	}})
	builder.WriteString(" ")
	return builder.String()
}

var reasoningSummaryPattern = regexp.MustCompile(`(?s)^\s*\*\*(.+?)\*\*\s*(?:\n+(.*))?$`)

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

func eventToolName(e agent.Event) string {
	if e.Tool != nil {
		return e.Tool.Name()
	}
	if e.ToolCall != nil {
		return e.ToolCall.Name
	}
	if e.ToolResult != nil {
		return e.ToolResult.Name
	}
	return ""
}

func normalizedToolName(e agent.Event) string {
	normalize := func(name string) string {
		name = strings.ToLower(strings.TrimSpace(name))
		// `skill_shell` behaves like `shell` for logic that groups them together.
		if name == "skill_shell" {
			return "shell"
		}
		return name
	}

	return normalize(eventToolName(e))
}

func toolDisplayName(e agent.Event) string {
	name := eventToolName(e)
	if name == "" {
		return "tool call"
	}
	return sanitizeText(name)
}

type presentedBodyLineKind int

const (
	presentedBodyLineStandard presentedBodyLineKind = iota + 1
	presentedBodyLineSection
	presentedBodyLinePatch
)

type presentedBodyLine struct {
	kind  presentedBodyLineKind
	runes []styledRune
	patch patchLine
}

func presenterPresentation(e agent.Event) (llmstream.Presentation, bool) {
	if e.Tool == nil || e.ToolCall == nil {
		return llmstream.Presentation{}, false
	}

	presenter := e.Tool.Presenter()
	if presenter == nil {
		return llmstream.Presentation{}, false
	}

	var result *llmstream.ToolResult
	if e.Type == agent.EventTypeToolComplete {
		result = e.ToolResult
	}

	presentation := presenter.Present(*e.ToolCall, result)
	if presentation.Behavior == "" {
		return llmstream.Presentation{}, false
	}
	if err := validatePresentedToolSummary(presentation); err != nil {
		return llmstream.Presentation{
			Behavior: presentation.Behavior,
			Summary: llmstream.Line{
				Segments: []llmstream.Segment{
					{Text: "Error", Role: llmstream.RoleError},
					{Text: " " + err.Error(), Role: llmstream.RoleNormal},
				},
			},
		}, true
	}
	if _, ok := presentationDiffBody(presentation); ok {
		return presentation, true
	}
	if len(presentation.Summary.Segments) == 0 {
		return llmstream.Presentation{}, false
	}

	return presentation, true
}

func validatePresentedToolSummary(presentation llmstream.Presentation) error {
	if _, ok := presentationDiffBody(presentation); ok && presentation.Summary.Segments != nil {
		return fmt.Errorf("presenter diff bodies must leave Summary.Segments nil")
	}
	return nil
}

func presentationDiffBody(presentation llmstream.Presentation) (llmstream.Diff, bool) {
	switch body := presentation.Body.(type) {
	case llmstream.Diff:
		return body, true
	case *llmstream.Diff:
		if body != nil {
			return *body, true
		}
	}
	return llmstream.Diff{}, false
}

func presentationSegmentStyle(role llmstream.SegmentRole) runeStyle {
	switch role {
	case llmstream.RoleAccent:
		return runeStyle{color: colorAccent}
	case llmstream.RoleAction:
		return runeStyle{color: colorColorful, bold: true}
	case llmstream.RoleSuccess:
		return runeStyle{color: colorGreen}
	case llmstream.RoleError:
		return runeStyle{color: colorRed}
	case llmstream.RoleCode:
		return runeStyle{color: colorAccent}
	case llmstream.RoleEmphasis:
		return runeStyle{color: colorNormal, italic: true}
	default:
		return runeStyle{color: colorNormal}
	}
}

func presentationLineSegments(line llmstream.Line) []textSegment {
	return presentationLineSegmentsWithTransform(line, nil)
}

func presentationLineSegmentsWithTransform(line llmstream.Line, transform func(runeStyle) runeStyle) []textSegment {
	if len(line.Segments) == 0 {
		return nil
	}

	segments := make([]textSegment, 0, len(line.Segments)*2-1)
	hasContent := false
	for _, segment := range line.Segments {
		if segment.Text == "" {
			continue
		}
		if line.JoinWithSpace && hasContent {
			segments = append(segments, textSegment{text: " "})
		}
		style := presentationSegmentStyle(segment.Role)
		if transform != nil {
			style = transform(style)
		}
		segments = append(segments, textSegment{
			text:  segment.Text,
			style: style,
		})
		hasContent = true
	}
	return segments
}

func (f *textTUIFormatter) tuiPresentedToolSummary(width int, bullet colorRole, presentation llmstream.Presentation) string {
	if diff, ok := presentationDiffBody(presentation); ok && len(diff.Edits) > 0 {
		return f.tuiBulletLine(width, bullet, applyPatchHeaderSegments(patchChangeFromPresentedDiffSummary(diff))...)
	}
	return f.tuiBulletLine(width, bullet, presentationLineSegments(presentation.Summary)...)
}

func (f *textTUIFormatter) cliPresentedToolSummary(bullet colorRole, presentation llmstream.Presentation) string {
	if diff, ok := presentationDiffBody(presentation); ok && len(diff.Edits) > 0 {
		return f.cliBulletLine(bullet, applyPatchHeaderSegments(patchChangeFromPresentedDiffSummary(diff))...)
	}
	return f.cliBulletLine(bullet, presentationLineSegments(presentation.Summary)...)
}

func presenterCompletionErrorOutput(result *llmstream.ToolResult) ([]toolOutputLine, bool) {
	if result == nil {
		return nil, false
	}
	if result.IsError {
		return summarizeToolResult(*result), true
	}

	trimmed := strings.TrimSpace(result.Result)
	if trimmed == "" {
		return nil, false
	}

	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, false
	}
	if strings.TrimSpace(payload.Error) == "" {
		return nil, false
	}
	return summarizeToolResult(*result), true
}

func presenterCompletionSuccess(e agent.Event, presentation llmstream.Presentation) (bool, []toolOutputLine) {
	success, outputLines := parseToolResult(e)
	switch presentation.Status {
	case llmstream.PresentationStatusSuccess:
		success = true
	case llmstream.PresentationStatusFailure:
		success = false
	}
	return success, outputLines
}

func (f *textTUIFormatter) presenterBodyLines(presentation llmstream.Presentation) []presentedBodyLine {
	switch body := presentation.Body.(type) {
	case llmstream.Paragraph:
		return f.presenterParagraphBlockLines(body)
	case *llmstream.Paragraph:
		if body != nil {
			return f.presenterParagraphBlockLines(*body)
		}
	case llmstream.Checklist:
		return f.presenterChecklistBlockLines(body)
	case *llmstream.Checklist:
		if body != nil {
			return f.presenterChecklistBlockLines(*body)
		}
	case llmstream.Diff:
		return f.presenterDiffBlockLines(body)
	case *llmstream.Diff:
		if body != nil {
			return f.presenterDiffBlockLines(*body)
		}
	case llmstream.Output:
		return f.presenterOutputBlockLines(body)
	case *llmstream.Output:
		if body != nil {
			return f.presenterOutputBlockLines(*body)
		}
	}
	return nil
}

func (f *textTUIFormatter) presenterParagraphBlockLines(paragraph llmstream.Paragraph) []presentedBodyLine {
	lines := make([]presentedBodyLine, 0, len(paragraph.Lines))
	for _, line := range paragraph.Lines {
		lines = append(lines, presentedBodyLine{
			kind:  presentedBodyLineStandard,
			runes: f.runesFromSegments(presentationLineSegments(line)...),
		})
	}
	return lines
}

func checklistMarkerStyle(segments []textSegment, fallback runeStyle) runeStyle {
	for _, segment := range segments {
		if segment.text != "" {
			return segment.style
		}
	}
	return fallback
}

func (f *textTUIFormatter) presenterChecklistBlockLines(checklist llmstream.Checklist) []presentedBodyLine {
	lines := make([]presentedBodyLine, 0, len(checklist.Items)+1)
	if overview := presentationLineSegments(checklist.Overview); len(overview) > 0 {
		lines = append(lines, presentedBodyLine{
			kind:  presentedBodyLineStandard,
			runes: f.runesFromSegments(overview...),
		})
	}
	for _, item := range checklist.Items {
		status := item.Status
		emphasize := func(style runeStyle) runeStyle {
			if status == llmstream.ChecklistStatusInProgress {
				style.bold = true
			}
			return style
		}

		lineSegments := presentationLineSegmentsWithTransform(item.Line, emphasize)
		markerStyle := checklistMarkerStyle(lineSegments, emphasize(runeStyle{color: colorAccent}))
		marker := "□ "
		if status == llmstream.ChecklistStatusCompleted {
			marker = "✔ "
		}
		segments := append([]textSegment{{text: marker, style: markerStyle}}, lineSegments...)
		lines = append(lines, presentedBodyLine{
			kind:  presentedBodyLineStandard,
			runes: f.runesFromSegments(segments...),
		})
	}
	return lines
}

func (f *textTUIFormatter) presenterDiffBlockLines(diff llmstream.Diff) []presentedBodyLine {
	var lines []presentedBodyLine
	for idx, edit := range diff.Edits {
		change := patchChangeFromPresentedDiffEdit(edit)
		switch idx {
		case 0:
			// The summary already owns the first edit header.
		default:
			lines = append(lines, presentedBodyLine{
				kind:  presentedBodyLineSection,
				runes: f.runesFromSegments(applyPatchHeaderSegments(change)...),
			})
		}
		for _, patchLine := range change.lines {
			lines = append(lines, presentedBodyLine{
				kind:  presentedBodyLinePatch,
				patch: patchLine,
			})
		}
		if edit.Error != nil {
			lines = append(lines, presentedBodyLine{
				kind:  presentedBodyLineStandard,
				runes: f.runesFromSegments(presentationLineSegments(*edit.Error)...),
			})
		}
	}
	return lines
}

func (f *textTUIFormatter) presenterOutputBlockLines(output llmstream.Output) []presentedBodyLine {
	lines := make([]presentedBodyLine, 0, len(output.Lines)+1)
	for _, line := range output.Lines {
		line = sanitizeText(line)
		runes := f.buildStyledRunes(line, runeStyle{color: colorAccent}, f.codeRanges(line))
		lines = append(lines, presentedBodyLine{
			kind:  presentedBodyLineStandard,
			runes: runes,
		})
	}
	if output.OmittedLineCount > 0 {
		lines = append(lines, presentedBodyLine{
			kind:  presentedBodyLineStandard,
			runes: f.buildStyledRunes(fmt.Sprintf("… +%d lines", output.OmittedLineCount), runeStyle{color: colorAccent}, nil),
		})
	}
	return lines
}

func (f *textTUIFormatter) appendPresentedBodyTUI(builder *strings.Builder, width int, bullet colorRole, lines []presentedBodyLine) {
	wroteStandard := false
	for _, line := range lines {
		builder.WriteByte('\n')
		switch line.kind {
		case presentedBodyLineSection:
			if len(line.runes) == 0 {
				builder.WriteString(f.bulletPrefix(bullet))
				continue
			}
			builder.WriteString(f.wrapStyledText(line.runes, width, f.bulletPrefix(bullet), "  "))
		case presentedBodyLinePatch:
			firstPrefix := "     "
			restPrefix := "       "
			if line.patch.kind == patchLineGap {
				restPrefix = firstPrefix
			}
			runes := f.buildPatchStyledRunes(line.patch)
			if len(runes) == 0 {
				builder.WriteString(firstPrefix)
				continue
			}
			builder.WriteString(f.wrapStyledText(runes, width, firstPrefix, restPrefix))
		default:
			firstPrefix := "    "
			if !wroteStandard {
				firstPrefix = f.toolOutputFirstPrefix()
				wroteStandard = true
			}
			if len(line.runes) == 0 {
				builder.WriteString(firstPrefix)
				continue
			}
			builder.WriteString(f.wrapStyledText(line.runes, width, firstPrefix, "    "))
		}
	}
}

func (f *textTUIFormatter) presentedBodyCLILines(bullet colorRole, lines []presentedBodyLine) []string {
	if len(lines) == 0 {
		return nil
	}
	result := make([]string, 0, len(lines))
	wroteStandard := false
	for _, line := range lines {
		switch line.kind {
		case presentedBodyLineSection:
			result = append(result, f.cliSimpleLine(line.runes, bullet))
		case presentedBodyLinePatch:
			runes := f.buildStyledRunes("     ", runeStyle{}, nil)
			runes = append(runes, f.buildPatchStyledRunes(line.patch)...)
			result = append(result, f.styledString(runes))
		default:
			prefix := f.cliToolOutputPrefix(!wroteStandard)
			wroteStandard = true
			runes := append([]styledRune{}, prefix...)
			runes = append(runes, line.runes...)
			result = append(result, f.styledString(runes))
		}
	}
	return result
}

func (f *textTUIFormatter) tuiToolCall(e agent.Event, width int) string {
	if presentation, ok := presenterPresentation(e); ok {
		var builder strings.Builder
		builder.WriteString(f.tuiPresentedToolSummary(width, colorAccent, presentation))
		if bodyLines := f.presenterBodyLines(presentation); len(bodyLines) > 0 {
			f.appendPresentedBodyTUI(&builder, width, colorAccent, bodyLines)
		}
		return builder.String()
	}

	switch normalizedToolName(e) {
	case "review":
		return f.tuiReviewToolCall(e, width)
	default:
		return f.tuiGenericToolCall(e, width)
	}
}

func (f *textTUIFormatter) cliToolCall(e agent.Event) string {
	if presentation, ok := presenterPresentation(e); ok {
		lines := []string{f.cliPresentedToolSummary(colorAccent, presentation)}
		if bodyLines := f.presenterBodyLines(presentation); len(bodyLines) > 0 {
			if rest := f.presentedBodyCLILines(colorAccent, bodyLines); len(rest) > 0 {
				lines = append(lines, rest...)
			}
		}
		return strings.Join(lines, "\n")
	}

	switch normalizedToolName(e) {
	case "review":
		return f.cliReviewToolCall(e)
	default:
		return f.cliGenericToolCall(e)
	}
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
	if f.isSillyAgentOutsidePackage(e) {
		return f.tuiSillyAgentOutsidePackage(e, width)
	}

	if presentation, ok := presenterPresentation(e); ok {
		success, outputLines := presenterCompletionSuccess(e, presentation)
		bullet := colorGreen
		if !success {
			bullet = colorRed
		}

		var builder strings.Builder
		builder.WriteString(f.tuiPresentedToolSummary(width, bullet, presentation))
		if presentation.ErrorBehavior != llmstream.ErrorBehaviorPresenterOwned {
			if errorLines, ok := presenterCompletionErrorOutput(e.ToolResult); ok {
				f.appendTUIToolOutput(&builder, width, errorLines)
				return builder.String()
			}
		}
		if bodyLines := f.presenterBodyLines(presentation); len(bodyLines) > 0 {
			f.appendPresentedBodyTUI(&builder, width, bullet, bodyLines)
		} else if presentation.ErrorBehavior != llmstream.ErrorBehaviorPresenterOwned && !success && len(outputLines) > 0 {
			f.appendTUIToolOutput(&builder, width, outputLines)
		}
		return builder.String()
	}

	success, outputLines := parseToolResult(e)
	switch normalizedToolName(e) {
	case "review":
		return f.tuiReviewToolComplete(e, width, success, outputLines)
	default:
		return f.tuiGenericToolComplete(e, width, success, outputLines)
	}
}

func (f *textTUIFormatter) cliToolComplete(e agent.Event) string {
	if f.isSillyAgentOutsidePackage(e) {
		return f.cliSillyAgentOutsidePackage(e)
	}

	if presentation, ok := presenterPresentation(e); ok {
		success, outputLines := presenterCompletionSuccess(e, presentation)
		bullet := colorGreen
		if !success {
			bullet = colorRed
		}

		lines := []string{f.cliPresentedToolSummary(bullet, presentation)}
		if presentation.ErrorBehavior != llmstream.ErrorBehaviorPresenterOwned {
			if errorLines, ok := presenterCompletionErrorOutput(e.ToolResult); ok {
				if rest := f.cliToolOutputLines(errorLines); len(rest) > 0 {
					lines = append(lines, rest...)
				}
				return strings.Join(lines, "\n")
			}
		}
		if bodyLines := f.presenterBodyLines(presentation); len(bodyLines) > 0 {
			if rest := f.presentedBodyCLILines(bullet, bodyLines); len(rest) > 0 {
				lines = append(lines, rest...)
			}
		} else if presentation.ErrorBehavior != llmstream.ErrorBehaviorPresenterOwned && !success {
			if rest := f.cliToolOutputLines(outputLines); len(rest) > 0 {
				lines = append(lines, rest...)
			}
		}
		return strings.Join(lines, "\n")
	}

	success, outputLines := parseToolResult(e)
	switch normalizedToolName(e) {
	case "review":
		return f.cliReviewToolComplete(e, success, outputLines)
	default:
		return f.cliGenericToolComplete(e, success, outputLines)
	}
}

func (f *textTUIFormatter) isSillyAgentOutsidePackage(e agent.Event) bool {
	if e.ToolResult == nil || e.ToolResult.SourceErr == nil {
		return false
	}
	return errors.Is(e.ToolResult.SourceErr, authdomain.ErrCodeUnitPathOutside)
}

func sillyAgentToolAndPath(e agent.Event) (tool string, path string, hasPath bool) {
	tool = strings.TrimSpace(normalizedToolName(e))
	if tool == "" {
		tool = strings.TrimSpace(toolDisplayName(e))
	}

	// Best-effort: many tools carry a simple {"path": "..."} payload.
	if e.ToolCall != nil {
		var payload struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(e.ToolCall.Input)), &payload); err == nil {
			p := strings.TrimSpace(payload.Path)
			if p != "" {
				return tool, sanitizeText(p), true
			}
		}
	}

	return tool, "", false
}

func (f *textTUIFormatter) tuiSillyAgentOutsidePackage(e agent.Event, width int) string {
	tool, path, hasPath := sillyAgentToolAndPath(e)
	msg := "Silly LLM tried " + tool
	if hasPath {
		msg += " on " + path
	}
	msg += " outside of package."

	runes := f.buildStyledRunes(sanitizeText(msg), runeStyle{color: colorAccent}, nil)
	return f.wrapStyledText(runes, width, f.bulletPrefix(colorRed), "  ")
}

func (f *textTUIFormatter) cliSillyAgentOutsidePackage(e agent.Event) string {
	tool, path, hasPath := sillyAgentToolAndPath(e)
	msg := "Silly LLM tried " + tool
	if hasPath {
		msg += " on " + path
	}
	msg += " outside of package."

	runes := f.buildStyledRunes(sanitizeText(msg), runeStyle{color: colorAccent}, nil)
	return f.cliSimpleLine(runes, colorRed)
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

func (f *textTUIFormatter) tuiGenericToolComplete(e agent.Event, width int, success bool, outputLines []toolOutputLine) string {
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

func (f *textTUIFormatter) cliGenericToolComplete(e agent.Event, success bool, outputLines []toolOutputLine) string {
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

// parseToolResult returns success and formatted output lines. Most tools use the default summarized output limit; tool-specific formatters can ignore the returned
// lines and re-summarize with different rules.
func parseToolResult(e agent.Event) (bool, []toolOutputLine) {
	success := true
	if e.ToolResult != nil {
		success = !e.ToolResult.IsError
	}

	var lines []toolOutputLine
	if e.ToolResult != nil {
		lines = summarizeToolResult(*e.ToolResult)
		if resultSuccess, ok := toolResultSuccess(*e.ToolResult); ok {
			success = resultSuccess
		}
	}
	return success, lines
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
	return summarizeToolResultWithMaxLines(result, 5)
}

func summarizeToolResultWithMaxLines(result llmstream.ToolResult, maxLines int) []toolOutputLine {
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
			return summarizeToolContentWithMaxLines(payload.Content, maxLines)
		}
	}

	if result.IsError {
		return []toolOutputLine{{
			text:          sanitizeText(fmt.Sprintf("Error: %s", trimmed)),
			style:         runeStyle{color: colorRed},
			highlightCode: false,
		}}
	}

	return summarizeToolContentWithMaxLines(trimmed, maxLines)
}

func summarizeToolContentWithMaxLines(content string, maxLines int) []toolOutputLine {
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

	var summarised []string
	if maxLines > 0 && len(lines) > maxLines {
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

func extractReview(call *llmstream.ToolCall) (base string, ok bool) {
	if call == nil {
		return "", false
	}
	var payload struct {
		Base string `json:"base"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(call.Input)), &payload); err != nil {
		return "", false
	}
	base = sanitizeText(strings.TrimSpace(payload.Base))
	if base == "" {
		return "", false
	}
	return base, true
}

func reviewHeaderSegments(verb string, base string) []textSegment {
	segments := []textSegment{
		{text: verb, style: runeStyle{color: colorColorful, bold: true}},
	}
	base = strings.TrimSpace(base)
	if base != "" {
		segments = append(segments, textSegment{text: " " + base})
	}
	return segments
}

const maxDisplayedReviewFindings = 5

type reviewToolFinding struct {
	Title *string `json:"title"`
}

type reviewToolPayload struct {
	Findings               *[]reviewToolFinding `json:"findings"`
	OverallCorrectness     *string              `json:"overall_correctness"`
	OverallExplanation     *string              `json:"overall_explanation"`
	OverallConfidenceScore *float64             `json:"overall_confidence_score"`
}

func reviewToolPayloadValid(payload reviewToolPayload) bool {
	if payload.Findings == nil || payload.OverallCorrectness == nil || payload.OverallExplanation == nil || payload.OverallConfidenceScore == nil {
		return false
	}

	overallCorrectness := strings.TrimSpace(*payload.OverallCorrectness)
	if overallCorrectness != "patch is correct" && overallCorrectness != "patch is incorrect" {
		return false
	}
	if strings.TrimSpace(*payload.OverallExplanation) == "" {
		return false
	}
	if *payload.OverallConfidenceScore < 0 || *payload.OverallConfidenceScore > 1 {
		return false
	}
	for _, finding := range *payload.Findings {
		if finding.Title == nil || strings.TrimSpace(*finding.Title) == "" {
			return false
		}
	}

	return true
}

func parseReviewToolPayload(content string) (reviewToolPayload, bool) {
	return parseReviewToolPayloadRaw([]byte(strings.TrimSpace(content)))
}

func parseReviewToolPayloadRaw(raw []byte) (reviewToolPayload, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return reviewToolPayload{}, false
	}

	var payload reviewToolPayload
	if err := json.Unmarshal(raw, &payload); err == nil && reviewToolPayloadValid(payload) {
		return payload, true
	}

	var wrappedString string
	if err := json.Unmarshal(raw, &wrappedString); err == nil {
		return parseReviewToolPayload(wrappedString)
	}

	var wrapper struct {
		Content json.RawMessage `json:"content"`
		Error   string          `json:"error"`
		Success *bool           `json:"success"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return reviewToolPayload{}, false
	}
	if wrapper.Success != nil && !*wrapper.Success {
		return reviewToolPayload{}, false
	}
	if strings.TrimSpace(wrapper.Error) != "" {
		return reviewToolPayload{}, false
	}
	if len(bytes.TrimSpace(wrapper.Content)) == 0 {
		return reviewToolPayload{}, false
	}

	return parseReviewToolPayloadRaw(wrapper.Content)
}

func reviewToolResultSuccess(result *llmstream.ToolResult) (bool, bool) {
	if result == nil {
		return false, false
	}
	if result.IsError {
		return false, true
	}

	trimmed := strings.TrimSpace(result.Result)
	if trimmed == "" {
		return true, true
	}

	var wrapper struct {
		Error   string `json:"error"`
		Success *bool  `json:"success"`
	}
	if err := json.Unmarshal([]byte(trimmed), &wrapper); err == nil {
		if wrapper.Success != nil {
			return *wrapper.Success, true
		}
		if strings.TrimSpace(wrapper.Error) != "" {
			return false, true
		}
		return true, true
	}

	return true, true
}

func reviewToolPayloadLines(payload reviewToolPayload) []toolOutputLine {
	findings := *payload.Findings
	if len(findings) == 0 {
		text := "No findings."
		if payload.OverallCorrectness != nil && strings.TrimSpace(*payload.OverallCorrectness) == "patch is correct" {
			text = "No findings. Patch is correct."
		}
		return []toolOutputLine{{
			text:          text,
			style:         runeStyle{color: colorAccent},
			highlightCode: false,
		}}
	}

	limit := len(findings)
	if limit > maxDisplayedReviewFindings {
		limit = maxDisplayedReviewFindings
	}

	lines := make([]toolOutputLine, 0, limit+1)
	for _, finding := range findings[:limit] {
		lines = append(lines, toolOutputLine{
			text:          strings.TrimSpace(*finding.Title),
			style:         runeStyle{color: colorAccent},
			highlightCode: true,
		})
	}
	if remaining := len(findings) - limit; remaining > 0 {
		lines = append(lines, toolOutputLine{
			text:          fmt.Sprintf("… +%d findings", remaining),
			style:         runeStyle{color: colorAccent},
			highlightCode: false,
		})
	}
	return lines
}

func summarizeReviewToolResult(result *llmstream.ToolResult) ([]toolOutputLine, bool) {
	if result == nil || result.IsError {
		return nil, false
	}

	trimmed := strings.TrimSpace(result.Result)
	if payload, ok := parseReviewToolPayload(trimmed); ok {
		return reviewToolPayloadLines(payload), true
	}
	return nil, false
}

func (f *textTUIFormatter) tuiReviewToolCall(e agent.Event, width int) string {
	base, ok := extractReview(e.ToolCall)
	if !ok {
		return f.tuiGenericToolCall(e, width)
	}
	return f.tuiBulletLine(width, colorAccent, reviewHeaderSegments("Reviewing", base)...)
}

func (f *textTUIFormatter) cliReviewToolCall(e agent.Event) string {
	base, ok := extractReview(e.ToolCall)
	if !ok {
		return f.cliGenericToolCall(e)
	}
	return f.cliBulletLine(colorAccent, reviewHeaderSegments("Reviewing", base)...)
}

func (f *textTUIFormatter) tuiReviewToolComplete(e agent.Event, width int, success bool, outputLines []toolOutputLine) string {
	base, ok := extractReview(e.ToolCall)
	if !ok {
		return f.tuiGenericToolComplete(e, width, success, outputLines)
	}
	if reviewSuccess, ok := reviewToolResultSuccess(e.ToolResult); ok {
		success = reviewSuccess
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	var builder strings.Builder
	builder.WriteString(f.tuiBulletLine(width, bullet, reviewHeaderSegments("Reviewed", base)...))
	if success {
		if reviewLines, ok := summarizeReviewToolResult(e.ToolResult); ok {
			outputLines = reviewLines
		}
	}
	if len(outputLines) > 0 {
		f.appendTUIToolOutput(&builder, width, outputLines)
	}
	return builder.String()
}

func (f *textTUIFormatter) cliReviewToolComplete(e agent.Event, success bool, outputLines []toolOutputLine) string {
	base, ok := extractReview(e.ToolCall)
	if !ok {
		return f.cliGenericToolComplete(e, success, outputLines)
	}
	if reviewSuccess, ok := reviewToolResultSuccess(e.ToolResult); ok {
		success = reviewSuccess
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	if success {
		if reviewLines, ok := summarizeReviewToolResult(e.ToolResult); ok {
			outputLines = reviewLines
		}
	}
	lines := []string{f.cliBulletLine(bullet, reviewHeaderSegments("Reviewed", base)...)}
	if rest := f.cliToolOutputLines(outputLines); len(rest) > 0 {
		lines = append(lines, rest...)
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
			useSpaceBreak := breakIndex > 0
			var firstPart []styledRune
			if useSpaceBreak {
				firstPart = trimTrailingSpaces(buffer[:breakIndex])
				if len(firstPart) == 0 {
					useSpaceBreak = false
				}
			}
			if !useSpaceBreak {
				breakIndex = len(buffer) - 1
				if breakIndex <= 0 {
					firstPart = append([]styledRune(nil), buffer...)
					emitLine(firstPart)
					if pad := continuationPaddingForLine(firstPart); pad != "" || continuationPadding == "" {
						continuationPadding = pad
					}
					buffer = nil
					currentWidth = 0
					lastSpace = -1
					updateLimit()
					continue
				}
				firstPart = append([]styledRune(nil), buffer[:breakIndex]...)
			}
			emitLine(firstPart)
			if pad := continuationPaddingForLine(firstPart); pad != "" || continuationPadding == "" {
				continuationPadding = pad
			}
			updateLimit()

			remainder := append([]styledRune(nil), buffer[breakIndex:]...)
			if useSpaceBreak {
				remainder = trimLeadingSpaces(remainder)
			}

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
