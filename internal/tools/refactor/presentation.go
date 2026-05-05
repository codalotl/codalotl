package refactor

import (
	"encoding/json"
	"fmt"

	"github.com/codalotl/codalotl/internal/llmstream"
)

type refactorPresenter struct{}

func (refactorPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	params, _ := parseParams(call.Input)
	name := params.Name
	pkg := params.Package
	var statusDetail string
	if result != nil {
		var toolResult Result
		if err := json.Unmarshal([]byte(result.Result), &toolResult); err == nil {
			if toolResult.Name != "" {
				name = toolResult.Name
			}
			if toolResult.Package != "" {
				pkg = toolResult.Package
			}
			statusDetail = refactorStatusDetail(toolResult.Status, toolResult.Message)
		}
	}
	if name == "" {
		name = "refactor"
	}
	if pkg == "" {
		pkg = "package"
	}

	verb := "Refactoring"
	if result != nil {
		verb = "Refactored"
	}
	presentation := llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorAppend,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: verb, Role: llmstream.RoleAction},
				{Text: name, Role: llmstream.RoleCode},
				{Text: "in", Role: llmstream.RoleAccent},
				{Text: pkg, Role: llmstream.RoleCode},
			},
		},
	}
	if result != nil && !result.IsError && statusDetail != "" {
		presentation.Body = llmstream.Paragraph{
			Lines: []llmstream.Line{
				{
					Segments: []llmstream.Segment{
						{Text: statusDetail, Role: llmstream.RoleSuccess},
					},
				},
			},
		}
	}
	return presentation
}

func refactorStatusDetail(status ResultStatus, message string) string {
	if message != "" {
		return sentenceCase(message)
	}
	switch status {
	case ResultStatusApplied:
		return "Successfully applied refactor"
	case ResultStatusNoOpportunity:
		return "No refactoring opportunities found"
	case ResultStatusAlreadyApplied:
		return "Refactor already applied"
	default:
		return ""
	}
}

func sentenceCase(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return fmt.Sprintf("%c%s", s[0]-('a'-'A'), s[1:])
}
