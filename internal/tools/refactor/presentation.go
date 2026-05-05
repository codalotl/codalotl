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
	if result != nil {
		var toolResult Result
		if err := json.Unmarshal([]byte(result.Result), &toolResult); err == nil {
			if toolResult.Name != "" {
				name = toolResult.Name
			}
			if toolResult.Package != "" {
				pkg = toolResult.Package
			}
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
	return llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorAppend,
		Summary: llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: fmt.Sprintf("%s %s in %s", verb, name, pkg)},
			},
		},
	}
}
