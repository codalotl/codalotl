package agent

import (
	"context"
	"sync"

	"github.com/codalotl/codalotl/internal/llmstream"
)

type toolOutputContextKey struct{}

type toolOutputEmitter struct {
	mu     sync.Mutex
	agent  *Agent
	out    chan<- Event
	tool   llmstream.Tool
	call   llmstream.ToolCall
	active bool
}

// EmitToolOutput emits display-only output for the active tool run. It is safe to call with any context.
func EmitToolOutput(ctx context.Context, content string) {
	if ctx == nil {
		return
	}
	emitter, _ := ctx.Value(toolOutputContextKey{}).(*toolOutputEmitter)
	if emitter == nil {
		return
	}
	emitter.emit(content)
}

func newToolOutputEmitter(agent *Agent, out chan<- Event, tool llmstream.Tool, call llmstream.ToolCall) *toolOutputEmitter {
	return &toolOutputEmitter{
		agent:  agent,
		out:    out,
		tool:   tool,
		call:   call,
		active: true,
	}
}

func withToolOutputContext(ctx context.Context, emitter *toolOutputEmitter) context.Context {
	if emitter == nil {
		return ctx
	}
	return context.WithValue(ctx, toolOutputContextKey{}, emitter)
}

func (e *toolOutputEmitter) emit(content string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.active || e.agent == nil || e.out == nil {
		return
	}

	callCopy := e.call
	e.agent.dispatchEvent(e.out, Event{
		Type:       EventTypeToolOutput,
		Tool:       e.tool,
		ToolCall:   &callCopy,
		ToolOutput: ToolOutput{Content: content},
	})
}

func (e *toolOutputEmitter) close() {
	e.mu.Lock()
	e.active = false
	e.mu.Unlock()
}
