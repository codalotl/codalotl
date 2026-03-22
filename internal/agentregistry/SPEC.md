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

## Public API

```go
// Registry holds agent and tool definitions.
type Registry struct {}

// NewRegistry returns a new registry.
func NewRegistry() *Registry

// Lookup returns the named Definition if it exists.
func (r *Registry) Lookup(name string) (Definition, bool)

// List returns all registered Definitions.
func (r *Registry) List() []Definition

// RegisterAgent adds or replaces a Definition by name.
func (r *Registry) RegisterAgent(def Definition) error

// Register adds or replaces a tool by toolName. toolset must return exactly one tool.
//
// TODO: in future, maybe define toolsetinterface.Tool that maps Options -> one func
func (r *Registry) RegisterTool(toolName string, toolset toolsetinterface.Toolset) error

// Register checks that all agents' references to tools are valid.
func (r *Registry) ValidateTools() error

func (r *Registry) Invoke(name string, message string) (<-chan agent.Event, error)


type PromptBuilderOptions struct {
    AgentName string
    toolsetinterface.Options
}

// PromptBuilder builds a prompt lazily based on options.
type PromptBuilder func(options PromptBuilderOptions) string

// Definition is an agent definition.
type Definition struct {
    // Name is the stable agent identifier.
    Name string

    // Description is surfaced in llmstream.ToolInfo.
    Description string

    // List of tools. Refers to tools added to a Registry.
    ToolNames []string

    SystemPrompt string

    // SystemPromptBuilder sets and overwrites SystemPrompt if non-nil.
    SystemPromptBuilder PromptBuilder
   
}

// Validate checks that a Definition is internally consistent.
//
// Validate only checks static definition shape. It does not resolve targets,
// render prompts, or construct tools.
func (d Definition) Validate() error


// RunRequest is the data needed to invoke an agent.
type RunRequest struct {
    // AgentCreator creates the agent (either a root or child agent).
    AgentCreator agent.AgentCreator

    // Model selects the model.
    Model llmmodel.ModelID

    // CallerAuthorizer is the current authorizer of the calling agent.
    CallerAuthorizer authdomain.Authorizer

    // CallerSandboxDir is the current sandbox root of the calling agent.
    // This is typically CallerAuthorizer.SandboxDir(), but is kept explicit so
    // callers do not need to reconstruct it if authorizer is nil in tests.
    CallerSandboxDir string

    // Input is the decoded JSON object for this invocation.
    // TODO: needed?
    Input map[string]any
}


```