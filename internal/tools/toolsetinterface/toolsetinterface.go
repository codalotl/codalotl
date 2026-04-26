// Package toolsetinterface defines shared types used to wire up toolsets without introducing import cycles.
//
// The motivating cycle is roughly:
//   - a tool (ex: update_usage) in tools/pkgtools wants to create a subagent in subagents/update_usage.
//   - the subagent wants access to package tools (ex: get_public_api) that also live in tools/pkgtools.
//
// Often this is solved by duplicating *interfaces* across packages, but these are function types. In Go, distinct named function types are not interchangeable even
// when they have the same signature. So this tiny package holds the shared types.
package toolsetinterface

import (
	"context"
	"encoding/json"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
)

// AgentInvoker is an interface that can create or invoke subagents by name.
type AgentInvoker interface {
	// Create creates a named agent with req.
	Create(ctx context.Context, agentName string, req InvokeRequest) (*agent.Agent, error)

	// Invoke creates and starts a named agent with req, returning a channel from which to read events. Use it as the one-shot convenience API when callers do not need
	// access to the created Agent.
	Invoke(ctx context.Context, agentName string, req InvokeRequest) (<-chan agent.Event, error)
}

// Options configures a Toolset.
type Options struct {
	// AgentName is the name of the agent currently being constructed.
	//
	// Tool constructors can use this to preserve agent-specific behavior when the same tool name is shared across multiple registered agents.
	AgentName string

	// SandboxDir is the sandbox root used to resolve relative paths provided by the LLM into absolute paths. The authorizer implements the actual access constraints
	// ("jail").
	SandboxDir string

	Authorizer authdomain.Authorizer

	// GoPkgAbsDir is the absolute path of the Go package directory that package-scoped toolsets operate on. It is required only for package-scoped toolsets.
	GoPkgAbsDir string

	// Model is the selected model identifier for the current run. Toolset constructors can use it to choose model/provider-specific tool behavior.
	Model llmmodel.ModelID

	// LintSteps are the linting steps that can be used by tools that need to check/fix formatting or other repo conventions.
	LintSteps []lints.Step

	// AgentInvoker allows a tool to create or invoke other agents.
	AgentInvoker AgentInvoker
}

// Toolset returns tools configured by opts.
type Toolset func(opts Options) ([]llmstream.Tool, error)

// Tool returns a single tool configured by opts.
type Tool func(opts Options) (llmstream.Tool, error)

// InvokeRequest is the data needed to create or invoke an agent.
type InvokeRequest struct {
	// ToolOptions supplies information needed to construct tools, such as GoPkgAbsDir, Authorizer, SandboxDir, Model.
	//
	// Any field supplied here is not duplicated elsewhere in InvokeRequest (ex: Model).
	ToolOptions Options

	AgentCreator       agent.AgentCreator    // AgentCreator creates the agent (either a root or child agent).
	CallerAuthorizer   authdomain.Authorizer // CallerAuthorizer is the current authorizer of the calling agent.
	CallerSandboxDir   string                // CallerSandboxDir is the current sandbox root of the calling agent.
	OverrideAuthorizer authdomain.Authorizer // If not nil, use this authorizer for the new agent.
	OverrideSandboxDir string                // If not "", use this sandbox dir for the new agent.
	Messages           []string              // Messages are the initial user messages to the LLM (after the prompt).
	Payload            json.RawMessage       // Optional JSON data that agents can use to construct prompts/context/initial turns.
}
