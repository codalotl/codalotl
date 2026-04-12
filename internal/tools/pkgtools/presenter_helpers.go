package pkgtools

import (
	"strings"

	"github.com/codalotl/codalotl/internal/llmstream"
)

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
