package agent

import (
	"context"
	"sync"

	"github.com/codalotl/codalotl/internal/llmstream"
)

// externalLLMUsageContextKey is the private context key for active tool LLM usage recorders.
type externalLLMUsageContextKey struct{}

// externalLLMUsageRecorder records usage for LLM calls made by a tool outside the agent conversation loop.
type externalLLMUsageRecorder struct {
	mu     sync.Mutex
	agent  *Agent
	active bool
}

// EmitExternalLLMUsage records token usage for an external LLM call. It is safe to call with any context.
func EmitExternalLLMUsage(ctx context.Context, usage llmstream.TokenUsage) {
	if ctx == nil {
		return
	}
	recorder, _ := ctx.Value(externalLLMUsageContextKey{}).(*externalLLMUsageRecorder)
	if recorder == nil {
		return
	}
	recorder.record(usage)
}

func newExternalLLMUsageRecorder(agent *Agent) *externalLLMUsageRecorder {
	return &externalLLMUsageRecorder{
		agent:  agent,
		active: true,
	}
}

func withExternalLLMUsageContext(ctx context.Context, recorder *externalLLMUsageRecorder) context.Context {
	if recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, externalLLMUsageContextKey{}, recorder)
}

func (r *externalLLMUsageRecorder) record(usage llmstream.TokenUsage) {
	r.mu.Lock()
	agent := r.agent
	active := r.active
	r.mu.Unlock()

	if !active || agent == nil {
		return
	}
	agent.addUsage(usage)
}

func (r *externalLLMUsageRecorder) close() {
	r.mu.Lock()
	r.active = false
	r.mu.Unlock()
}
