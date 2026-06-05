package tui

import (
	"context"

	"github.com/codalotl/codalotl/internal/llmstream"
)

// namedTool is a minimal llmstream.Tool implementation with a name and optional presenter.
type namedTool struct {
	name      string              // Name is returned in tool metadata and results.
	presenter llmstream.Presenter // Presenter customizes semantic formatting for calls and results.
}

func newNamedTool(name string) llmstream.Tool {
	return namedTool{name: name}
}

func newNamedToolWithPresenter(name string, presenter llmstream.Presenter) llmstream.Tool {
	return namedTool{name: name, presenter: presenter}
}

// Info returns tool metadata containing the configured tool name.
func (t namedTool) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{Name: t.name}
}

// Name returns the tool name used in metadata and results.
func (t namedTool) Name() string {
	return t.name
}

// Presenter returns the tool presenter, or nil when the tool uses default formatting.
func (t namedTool) Presenter() llmstream.Presenter {
	return t.presenter
}

// Run returns an empty successful tool result named for the configured tool.
func (t namedTool) Run(context.Context, llmstream.ToolCall) llmstream.ToolResult {
	return llmstream.ToolResult{Name: t.name}
}
