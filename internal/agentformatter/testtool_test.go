package agentformatter

import (
	"context"

	"github.com/codalotl/codalotl/internal/llmstream"
)

type fakeTool struct {
	name string
}

func (t fakeTool) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{Name: t.name}
}

func (t fakeTool) Name() string {
	return t.name
}

func (t fakeTool) Presenter() llmstream.Presenter {
	return nil
}

func (t fakeTool) Run(context.Context, llmstream.ToolCall) llmstream.ToolResult {
	return llmstream.ToolResult{}
}

func testTool(name string) llmstream.Tool {
	return fakeTool{name: name}
}
