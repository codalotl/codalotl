package exttools

import (
	"encoding/json"
	"strings"

	"github.com/codalotl/codalotl/internal/llmstream"
)

type extToolPayload struct {
	Content string `json:"content"`
	Error   string `json:"error"`
	Success *bool  `json:"success"`
}

func extToolSummaryPresentation(action string, target string) llmstream.Presentation {
	segments := []llmstream.Segment{{
		Text: action,
		Role: llmstream.RoleAction,
	}}
	if target != "" {
		segments = append(segments, llmstream.Segment{
			Text: target,
			Role: llmstream.RoleNormal,
		})
	}

	return llmstream.Presentation{
		Behavior:       llmstream.CompletionBehaviorReplace,
		NarrowBehavior: llmstream.PresentationNarrowBehaviorPreferCLI,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments:      segments,
		},
	}
}

func extToolResultPayloadContent(result llmstream.ToolResult) (string, extToolPayload, bool) {
	trimmed := strings.TrimSpace(result.Result)
	if trimmed == "" {
		return "", extToolPayload{}, false
	}

	var payload extToolPayload
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		if payload.Content != "" {
			return payload.Content, payload, true
		}
		if payload.Error != "" {
			return payload.Error, payload, true
		}
		return "", payload, false
	}

	return trimmed, extToolPayload{}, true
}

func extToolResultContent(result llmstream.ToolResult) (string, bool) {
	content, _, ok := extToolResultPayloadContent(result)
	return content, ok
}

func extToolResultSuccess(result llmstream.ToolResult) (bool, bool) {
	trimmed := strings.TrimSpace(result.Result)
	if trimmed == "" {
		if result.IsError {
			return false, true
		}
		return false, false
	}

	var payload extToolPayload
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		if payload.Success != nil {
			return *payload.Success, true
		}
		if strings.TrimSpace(payload.Error) != "" {
			return false, true
		}
		if ok, found := extractXMLishOK(payload.Content); found {
			return ok, true
		}
		if result.IsError {
			return false, true
		}
		return false, false
	}

	if ok, found := extractXMLishOK(trimmed); found {
		return ok, true
	}
	if result.IsError {
		return false, true
	}
	return false, false
}

func extractXMLishOK(s string) (bool, bool) {
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
	value := rest[1 : 1+closing]
	if strings.EqualFold(value, "true") {
		return true, true
	}
	if strings.EqualFold(value, "false") {
		return false, true
	}
	return false, false
}

func summarizePresenterOutput(content string, maxLines int) (llmstream.Output, bool) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")

	start := 0
	for i, line := range lines {
		if strings.TrimSpace(line) == "Output:" {
			start = i + 1
			break
		}
	}

	lines = trimPresenterEmptyLines(lines[start:])
	if len(lines) == 0 {
		return llmstream.Output{}, false
	}

	omitted := 0
	if maxLines > 0 && len(lines) > maxLines {
		omitted = len(lines) - maxLines
		lines = lines[:maxLines]
	}

	return llmstream.Output{
		Lines:            lines,
		OmittedLineCount: omitted,
	}, true
}

func trimPresenterEmptyLines(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func stripOuterXMLTag(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 3 || s[0] != '<' {
		return s
	}
	gt := strings.IndexByte(s, '>')
	if gt <= 1 {
		return s
	}

	tagPart := s[1:gt]
	for i, r := range tagPart {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
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

	return strings.TrimSpace(s[gt+1 : len(s)-len(closeTag)])
}
