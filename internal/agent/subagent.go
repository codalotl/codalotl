package agent

import (
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"context"
	"sync"
)

// AgentCreator can construct either a root Agent or a SubAgent, depending on how it was obtained.
type AgentCreator interface {
	New(model llmmodel.ModelID, systemPrompt string, tools []llmstream.Tool) (*Agent, error)
	NewWithDefaultModel(systemPrompt string, tools []llmstream.Tool) (*Agent, error)
}

// SubAgentCreator constructs SubAgents while servicing a tool call.
type SubAgentCreator interface {
	AgentCreator
}

// NewAgentCreator returns an AgentCreator that constructs root agents.
func NewAgentCreator() AgentCreator {
	return &defaultAgentCreator{
		defaultModel: llmmodel.ModelIDOrFallback(llmmodel.ModelIDUnknown),
	}
}

type defaultAgentCreator struct {
	defaultModel llmmodel.ModelID
}

func (c *defaultAgentCreator) New(model llmmodel.ModelID, systemPrompt string, tools []llmstream.Tool) (*Agent, error) {
	return NewAgent(model, systemPrompt, tools)
}

func (c *defaultAgentCreator) NewWithDefaultModel(systemPrompt string, tools []llmstream.Tool) (*Agent, error) {
	model := llmmodel.ModelIDOrFallback(c.defaultModel)
	return NewAgent(model, systemPrompt, tools)
}

type subAgentFactory struct {
	mu           sync.Mutex
	parent       *Agent
	parentOut    chan<- Event
	defaultModel llmmodel.ModelID
	tools        []llmstream.Tool
	closed       bool
}

func newSubAgentFactory(parent *Agent) *subAgentFactory {
	if parent == nil {
		return nil
	}

	parent.mu.Lock()
	out := parent.currentOut
	if parent.parentOut != nil {
		out = parent.parentOut
	}
	parent.mu.Unlock()

	if out == nil {
		panic("agent: subagent creation requested outside active run")
	}

	return &subAgentFactory{
		parent:       parent,
		parentOut:    out,
		defaultModel: parent.model,
		tools:        cloneToolSlice(parent.toolList),
	}
}

func (f *subAgentFactory) New(model llmmodel.ModelID, systemPrompt string, tools []llmstream.Tool) (*Agent, error) {
	return f.create(model, systemPrompt, tools)
}

func (f *subAgentFactory) NewWithDefaultModel(systemPrompt string, tools []llmstream.Tool) (*Agent, error) {
	model := llmmodel.ModelIDOrFallback(f.defaultModel)
	return f.create(model, systemPrompt, tools)
}

func (f *subAgentFactory) create(model llmmodel.ModelID, systemPrompt string, tools []llmstream.Tool) (*Agent, error) {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		panic("agent: subagent creator used after tool run completed")
	}

	parent := f.parent
	parentOut := f.parentOut
	f.mu.Unlock()

	if parentOut == nil {
		panic("agent: subagent creator missing parent output channel")
	}

	agentID, err := generateSessionID()
	if err != nil {
		return nil, err
	}

	child, err := newAgentInstance(model, systemPrompt, tools, parent.sessionID, agentID, parent, parent.depth+1, parentOut)
	if err != nil {
		return nil, err
	}
	return child, nil
}

func (f *subAgentFactory) Close() {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
}

func withSubAgentContext(ctx context.Context, factory *subAgentFactory, depth int) context.Context {
	if factory == nil {
		return ctx
	}
	values := &toolContextValues{
		depth:   depth,
		tools:   cloneToolSlice(factory.tools),
		creator: factory,
	}
	return context.WithValue(ctx, toolContextKey{}, values)
}

// SubAgentCreatorFromContext retrieves the SubAgentCreator registered for a tool run.
func SubAgentCreatorFromContext(ctx context.Context) SubAgentCreator {
	if ctx == nil {
		panic("agent: SubAgentCreatorFromContext called with nil context")
	}
	v := ctx.Value(toolContextKey{})
	if v == nil {
		panic("agent: SubAgentCreator not available in context")
	}
	tc, ok := v.(*toolContextValues)
	if !ok || tc.creator == nil {
		panic("agent: SubAgentCreator not available in context")
	}
	return tc.creator
}

// SubAgentDepth reports how many levels of subagents exist above the tool invocation.
// Returns -1 if the context is not associated with an agent tool invocation.
func SubAgentDepth(ctx context.Context) int {
	if ctx == nil {
		return -1
	}
	if v, ok := ctx.Value(toolContextKey{}).(*toolContextValues); ok {
		return v.depth
	}
	return -1
}

// AgentToolsFromContext returns a copy of the tool set available to the agent servicing the tool call.
func AgentToolsFromContext(ctx context.Context) []llmstream.Tool {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(toolContextKey{}).(*toolContextValues); ok {
		return cloneToolSlice(v.tools)
	}
	return nil
}

type toolContextKey struct{}

type toolContextValues struct {
	depth   int
	tools   []llmstream.Tool
	creator *subAgentFactory
}

var _ AgentCreator = (*defaultAgentCreator)(nil)
var _ AgentCreator = (*subAgentFactory)(nil)
var _ SubAgentCreator = (*subAgentFactory)(nil)
