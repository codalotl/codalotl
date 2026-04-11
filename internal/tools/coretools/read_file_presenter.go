package coretools

import (
	"encoding/json"
	"strings"

	"github.com/codalotl/codalotl/internal/llmstream"
)

var readFilePresenterInstance llmstream.Presenter = readFilePresenter{}

type readFilePresenter struct{}

func (p readFilePresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	_ = result

	return llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: "Read", Role: llmstream.RoleAction},
				{Text: readFilePresenterTarget(call), Role: llmstream.RoleNormal},
			},
		},
	}
}

func readFilePresenterTarget(call llmstream.ToolCall) string {
	var params ParamsReadFile
	if err := json.Unmarshal([]byte(call.Input), &params); err == nil {
		if path := strings.TrimSpace(params.Path); path != "" {
			return " " + path
		}
	}

	name := strings.TrimSpace(call.Name)
	if name == "" {
		name = ToolNameReadFile
	}
	return " " + name
}
