package pkgtools

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/codalotl/codalotl/internal/llmstream"
)

var usageResultPattern = regexp.MustCompile(`^\d+:`)

type pkgToolResultEnvelope struct {
	Content string `json:"content"`
	Error   string `json:"error"`
}

func pkgToolPresenterFallbackSummary(call llmstream.ToolCall) llmstream.Line {
	segments := []llmstream.Segment{
		{Text: "Tool", Role: llmstream.RoleAction},
	}
	if name := strings.TrimSpace(call.Name); name != "" {
		segments = append(segments, llmstream.Segment{Text: name, Role: llmstream.RoleNormal})
	}
	if input := strings.TrimSpace(call.Input); input != "" {
		segments = append(segments, llmstream.Segment{Text: input, Role: llmstream.RoleNormal})
	}
	return llmstream.Line{
		JoinWithSpace: true,
		Segments:      segments,
	}
}

func pkgToolPresenterOutput(content string) (llmstream.Output, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return llmstream.Output{}, false
	}

	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return llmstream.Output{
		Lines: strings.Split(content, "\n"),
	}, true
}

func pkgToolReplaceSummaryPresentation(summary llmstream.Line) llmstream.Presentation {
	return llmstream.Presentation{
		Behavior:       llmstream.CompletionBehaviorReplace,
		NarrowBehavior: llmstream.PresentationNarrowBehaviorPreferCLI,
		Summary:        summary,
	}
}

func pkgToolActionSummary(action string, trailing ...llmstream.Segment) llmstream.Line {
	segments := []llmstream.Segment{
		{Text: action, Role: llmstream.RoleAction},
	}
	segments = append(segments, trailing...)
	return llmstream.Line{
		JoinWithSpace: true,
		Segments:      segments,
	}
}

func pkgToolAccentParagraph(text string) (llmstream.Paragraph, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return llmstream.Paragraph{}, false
	}
	return llmstream.Paragraph{
		Lines: []llmstream.Line{{
			Segments: []llmstream.Segment{
				{Text: text, Role: llmstream.RoleAccent},
			},
		}},
	}, true
}

func pkgToolResultOutput(result llmstream.ToolResult) (llmstream.Output, bool) {
	if result.IsError {
		return llmstream.Output{}, false
	}

	content, payloadErr, isPayload := pkgToolResultPayloadContent(result)
	if isPayload && payloadErr != "" {
		return llmstream.Output{}, false
	}
	return pkgToolPresenterOutput(content)
}

func pkgToolResultPayloadContent(result llmstream.ToolResult) (string, string, bool) {
	trimmed := strings.TrimSpace(result.Result)
	if trimmed == "" {
		return "", "", false
	}

	payload, ok := parsePkgToolResultEnvelope(trimmed)
	if ok {
		return strings.TrimSpace(payload.Content), strings.TrimSpace(payload.Error), true
	}

	return trimmed, "", false
}

func parsePkgToolResultEnvelope(raw string) (pkgToolResultEnvelope, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || fields == nil {
		return pkgToolResultEnvelope{}, false
	}

	hasEnvelopeField := false
	for key := range fields {
		switch key {
		case "content", "error":
			hasEnvelopeField = true
		case "success":
			continue
		default:
			return pkgToolResultEnvelope{}, false
		}
	}
	if !hasEnvelopeField {
		return pkgToolResultEnvelope{}, false
	}

	var payload pkgToolResultEnvelope
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return pkgToolResultEnvelope{}, false
	}
	return payload, true
}

func pkgToolUsageResultCount(result llmstream.ToolResult) (int, bool) {
	if result.IsError {
		return 0, false
	}

	content, payloadErr, isPayload := pkgToolResultPayloadContent(result)
	if isPayload && payloadErr != "" {
		return 0, false
	}
	if !isPayload {
		content = strings.TrimSpace(result.Result)
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return 0, true
	}

	count := 0
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	for _, line := range strings.Split(content, "\n") {
		if usageResultPattern.MatchString(line) {
			count++
		}
	}
	return count, true
}

func pkgToolUsageSummaryLine(count int) llmstream.Paragraph {
	noun := "results"
	if count == 1 {
		noun = "result"
	}
	body, _ := pkgToolAccentParagraph(fmt.Sprintf("Found %d %s.", count, noun))
	return body
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
