# agentregistry

agentregistry allows the definition and registration of named agents:
- name
- prompt
- tools
- optional package
- authdomain configuration

These agents can then be easily invoked, including as subagents from tools.

## Dependencies

Dependencies are very important here, since agent -> tool -> agent must be supported. This package is intended to be lightweight, with no dependencies on concrete tool implementations or "nexus" packages.

Allowed deps:
- `internal/agent`
- `internal/llmstream`
- `internal/tools/authdomain`
- `internal/tools/toolsetinterface`

Package-specific context gathering should be injected via builders in a higher-level package. For instance, a package-mode agent may use `initialcontext`, `agentsmd`, env info, and skill loading, but those deps should not be added here.

## Supporting Agent -> Tool -> Agent

Agents need to be able to invoke other agents. However, they do so through tools. These tools may have custom code/functionality to create custom contexts, custom Authorizors, and so on. That being said, this package should encapsulate common patterns: make it easy to create package mode agents and tools.
- Example: consider something like `change_api`, which is a tool to make a change in a different package. This implementation can be split between a ChangeAPI tool and a `change_api` named agent. Part of the code can go in one, and part in another. Not all code need be in the `change_api` agent itself.

This package must offer affordances to allow the easy creation of tools based on agents. It must be possible to create simple agents (invokable as tools) with custom prompts and tool lists without writing custom code (it's acceptable if another package implements the glue code of config file -> agent+tool creation).

This package should also support named agents that prepare their own initial turns before the first message is sent. This is intended as injection point for context gathering such as package initial context, env info, AGENTS.md, etc.

Because of this, the `toolsetinterface` package contains `AgentInvoker` and `InvokeRequest` - types that conceptually belong here.
- TODO: should we merge toolsetinterface into this package?

## Public API

```go
// Registry holds agent and tool definitions.
type Registry struct{}

// NewRegistry returns a new registry.
func NewRegistry() *Registry

// Lookup returns the named Definition if it exists.
func (r *Registry) Lookup(agentName string) (Definition, bool)

// List returns all registered Definitions.
func (r *Registry) List() []Definition

// RegisterAgent adds or replaces a Definition by name.
func (r *Registry) RegisterAgent(def Definition) error

// RegisterTool adds or replaces a tool by toolName.
func (r *Registry) RegisterTool(toolName string, tool toolsetinterface.Tool) error

// ValidateTools checks that all agents' references to tools are valid.
func (r *Registry) ValidateTools() error

// Invoke begins executing the named agent, and returns a channel from which to read events.
func (r *Registry) Invoke(ctx context.Context, agentName string, req toolsetinterface.InvokeRequest) (<-chan agent.Event, error)

type BuildOptions struct {
	AgentName   string
	ToolOptions toolsetinterface.Options       // ToolOptions are the effective options used to construct tools after auth/package policy and overrides are applied.
	Request     toolsetinterface.InvokeRequest // Request is the original invocation request.
}

// ToolsBuilder returns tool names based on opts. It can be used to dynamically switch toolsets based on things like model.
type ToolsBuilder func(opts toolsetinterface.Options) ([]string, error)

// PromptBuilder builds a prompt lazily based on options.
type PromptBuilder func(options BuildOptions) (string, error)

// InitialTurnsBuilder builds user turns that are added before Request.Messages are sent.
type InitialTurnsBuilder func(ctx context.Context, options BuildOptions) ([]string, error)

type AuthPolicy string

const (
	// AuthPolicyDefault inherits (directly uses) the authorizer of caller, unless an override is set.
	AuthPolicyDefault AuthPolicy = ""

	// AuthPolicyPackage creates a new Authorizer based on the InvokeRequest's ToolOptions's GoPkgAbsDir while preserving the incoming SandboxDir. An error occurs if
	// an override is set.
	AuthPolicyPackage AuthPolicy = "package"
)

// Definition is an agent definition.
type Definition struct {
	Name                string        // Name is the stable agent identifier.
	Description         string        // Description is surfaced in llmstream.ToolInfo.
	ToolNames           []string      // List of tools. Refers to tools added to a Registry.
	ToolsBuilder        ToolsBuilder  // Additional tools to use (dynamically appended to ToolNames) based on invokation options (ex: model).
	SystemPrompt        string        // SystemPrompt to use if SystemPromptBuilder is nil.
	SystemPromptBuilder PromptBuilder // SystemPromptBuilder sets and overwrites SystemPrompt if non-nil.

	// InitialTurnsBuilder builds additional user turns before InvokeRequest.Messages are sent.
	//
	// This can gather context lazily based on invocation details. Example: package-mode agents may add AGENTS.md, env info, or initial package context.
	InitialTurnsBuilder InitialTurnsBuilder

	// AuthPolicy indicates how auth and package scoping are derived.
	AuthPolicy AuthPolicy
}

// Validate checks that a Definition is internally consistent.
//
// Validate only checks static definition shape. It does not resolve targets, render prompts, or construct tools.
func (d Definition) Validate() error
```
