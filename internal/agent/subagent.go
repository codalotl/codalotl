package agent

import (
	"context"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"sync"
)

// AgentCreator can construct either a root Agent or a SubAgent, depending on how it was obtained.
type AgentCreator interface {
	// Model omitted: root creators use their configured/default model; SubAgent creators use parent model.
	New(systemPrompt string, tools []llmstream.Tool, options ...NewOptions) (*Agent, error)
}

// SubAgentCreator constructs SubAgents while servicing a tool call.
type SubAgentCreator interface {
	// AgentCreator provides New for child agents bound to the active tool call.
	AgentCreator
}

// NewAgentCreator returns an AgentCreator that constructs root agents.
func NewAgentCreator(options ...NewOptions) AgentCreator {
	return &defaultAgentCreator{
		defaults: mergeNewOptions(options),
	}
}

// defaultAgentCreator constructs root agents using configured default options.
type defaultAgentCreator struct {
	defaults NewOptions // Defaults are merged into each new root agent; NoStore stays true once set.
}

// New constructs a root Agent using the creator's default options merged with call-specific options. NoStore remains true if either the defaults or call-specific
// options set it.
func (c *defaultAgentCreator) New(systemPrompt string, tools []llmstream.Tool, options ...NewOptions) (*Agent, error) {
	opts := make([]NewOptions, 0, 1+len(options))
	opts = append(opts, c.defaults)
	opts = append(opts, options...)
	return New(systemPrompt, tools, mergeNewOptions(opts))
}

// A subAgentFactory creates subagents for one active tool call and manages their lifetime.
//
// The factory is valid only while its owning tool call is running. Closing it prevents new subagents, cancels existing children, and waits for active child runs
// to finish.
type subAgentFactory struct {
	mu           sync.Mutex            // The mutex protects mutable factory lifecycle state.
	parent       *Agent                // The parent owns the tool call that may create subagents.
	parentOut    chan<- Event          // The parent output stream receives relayed child events.
	toolCallID   string                // The tool-call ID identifies the parent tool call that created child agents.
	defaultModel llmmodel.ModelID      // The default model is used when subagent options do not specify a model.
	tools        []llmstream.Tool      // The tools list stores the inherited tool set exposed to tool contexts.
	closed       bool                  // The closed flag prevents new child agents and runs after shutdown begins.
	children     map[*Agent]struct{}   // The children set tracks all created child agents for cancellation on close.
	active       map[*Agent]chan Event // The active map tracks running child agents and the streams CloseAndWait must drain.
	wg           sync.WaitGroup        // The wait group waits for active child runs to finish.
}

// newSubAgentFactory creates a factory for subagents scoped to toolCallID on parent.
func newSubAgentFactory(parent *Agent, toolCallID string) *subAgentFactory {
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
		toolCallID:   toolCallID,
		defaultModel: parent.model,
		tools:        cloneToolSlice(parent.toolList),
	}
}

// New creates a subagent scoped to the factory's active tool call.
func (f *subAgentFactory) New(systemPrompt string, tools []llmstream.Tool, options ...NewOptions) (*Agent, error) {
	resolved := mergeNewOptions(options)
	model := resolved.Model
	if model == "" {
		model = llmmodel.ModelIDOrFallback(f.defaultModel)
	}
	return f.create(model, systemPrompt, tools, resolved.NoStore, resolved.SubagentLabel)
}

// create constructs and registers a child Agent for the factory's active tool call. It panics if the factory is closed or missing its parent output channel.
func (f *subAgentFactory) create(model llmmodel.ModelID, systemPrompt string, tools []llmstream.Tool, noStore bool, subagentLabel string) (*Agent, error) {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		panic("agent: subagent creator used after tool run completed")
	}

	parent := f.parent
	parentOut := f.parentOut
	toolCallID := f.toolCallID
	f.mu.Unlock()

	if parentOut == nil {
		panic("agent: subagent creator missing parent output channel")
	}

	agentID, err := generateSessionID()
	if err != nil {
		return nil, err
	}

	lifetimeCtx, lifetimeCancel := context.WithCancel(context.Background())
	child, err := newAgentInstance(model, systemPrompt, tools, parent.sessionID, agentID, parent, parent.depth+1, parentOut, parent.noStore || noStore, subagentLabel, toolCallID)
	if err != nil {
		lifetimeCancel()
		return nil, err
	}
	child.subagentFactory = f
	child.lifetimeCtx = lifetimeCtx
	child.lifetimeCancel = lifetimeCancel

	f.registerChild(child)
	return child, nil
}

// registerChild records child for factory lifecycle tracking. It cancels child and panics if the factory has already been closed.
func (f *subAgentFactory) registerChild(child *Agent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		if child.lifetimeCancel != nil {
			child.lifetimeCancel()
		}
		panic("agent: subagent creator used after tool run completed")
	}
	if f.children == nil {
		f.children = make(map[*Agent]struct{})
	}
	f.children[child] = struct{}{}
}

// registerRun records child and its event stream as an active subagent run. It returns false if the factory has been closed; a successful registration must be finished
// so CloseAndWait can unblock.
func (f *subAgentFactory) registerRun(child *Agent, out chan Event) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return false
	}
	if f.active == nil {
		f.active = make(map[*Agent]chan Event)
	}
	f.active[child] = out
	f.wg.Add(1)
	return true
}

// finishRun marks child as no longer active so CloseAndWait can return when all child runs have ended.
func (f *subAgentFactory) finishRun(child *Agent) {
	f.mu.Lock()
	if _, ok := f.active[child]; ok {
		delete(f.active, child)
		f.wg.Done()
	}
	f.mu.Unlock()
}

// CloseAndWait closes the factory to new subagent runs, cancels all child agents, and waits for active runs to finish. It drains active child event streams while
// waiting so children can finish emitting cancellation and terminal events.
func (f *subAgentFactory) CloseAndWait() {
	f.mu.Lock()
	f.closed = true
	children := make([]*Agent, 0, len(f.children))
	for child := range f.children {
		children = append(children, child)
	}
	active := make([]chan Event, 0, len(f.active))
	for _, out := range f.active {
		active = append(active, out)
	}
	f.mu.Unlock()

	for _, child := range children {
		if child.lifetimeCancel != nil {
			child.lifetimeCancel()
		}
	}

	var drainWG sync.WaitGroup
	for _, out := range active {
		drainWG.Add(1)
		go func() {
			defer drainWG.Done()
			for range out {
			}
		}()
	}

	f.wg.Wait()
	drainWG.Wait()
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

// SubAgentDepth reports how many levels of subagents exist above the tool invocation. Returns -1 if the context is not associated with an agent tool invocation.
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

// toolContextKey is the private context key for agent tool-invocation values.
type toolContextKey struct{}

// toolContextValues stores agent metadata and helpers attached to the context passed to a tool run.
type toolContextValues struct {
	depth   int              // Depth is the nesting depth of the agent running the tool; root-agent tools use 0.
	tools   []llmstream.Tool // Tools contains the tool set available to the agent running the tool.
	creator *subAgentFactory // Creator creates subagents scoped to the active tool call.
}

var _ AgentCreator = (*defaultAgentCreator)(nil)
var _ AgentCreator = (*subAgentFactory)(nil)
var _ SubAgentCreator = (*subAgentFactory)(nil)
