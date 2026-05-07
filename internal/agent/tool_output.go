package agent

import (
	"context"
	"sync"

	"github.com/codalotl/codalotl/internal/llmstream"
)

// toolOutputContextKey is the private context key for active tool output emitters.
type toolOutputContextKey struct{}

// toolOutputEmitter sends display-only output events for a single tool call while the tool is running.
type toolOutputEmitter struct {
	mu     sync.Mutex         // Protects active and serializes event emission.
	agent  *Agent             // Dispatches tool-output events for the running tool.
	out    chan<- Event       // Receives tool-output events for the owning agent run.
	tool   llmstream.Tool     // Identifies the running tool associated with emitted output.
	call   llmstream.ToolCall // Identifies the provider tool call associated with emitted output.
	active bool               // Controls whether calls to emit produce events.
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

// emit sends content as an EventTypeToolOutput event for the emitter's tool call. It is a no-op after close or when the emitter is not attached to an agent stream.
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

// close marks the emitter inactive so future output is ignored. It is safe to call more than once and does not close the event channel.
func (e *toolOutputEmitter) close() {
	e.mu.Lock()
	e.active = false
	e.mu.Unlock()
}
