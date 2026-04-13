package tui

import (
	"context"

	"github.com/codalotl/codalotl/internal/llmstream"
)

type namedTool struct {
	name      string
	presenter llmstream.Presenter
}

func newNamedTool(name string) llmstream.Tool {
	return namedTool{name: name}
}

func newNamedToolWithPresenter(name string, presenter llmstream.Presenter) llmstream.Tool {
	return namedTool{name: name, presenter: presenter}
}

func (t namedTool) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{Name: t.name}
}

func (t namedTool) Name() string {
	return t.name
}

func (t namedTool) Presenter() llmstream.Presenter {
	return t.presenter
}

func (t namedTool) Run(context.Context, llmstream.ToolCall) llmstream.ToolResult {
	return llmstream.ToolResult{Name: t.name}
}
