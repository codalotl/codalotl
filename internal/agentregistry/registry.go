package agentregistry

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

// Registry holds agent and tool definitions.
type Registry struct {
	agents map[string]Definition
	tools  map[string]toolsetinterface.Tool
}

// NewRegistry returns a new registry.
func NewRegistry() *Registry {
	return &Registry{
		agents: make(map[string]Definition),
		tools:  make(map[string]toolsetinterface.Tool),
	}
}

// Lookup returns the named Definition if it exists.
func (r *Registry) Lookup(agentName string) (Definition, bool) {
	def, ok := r.agents[agentName]
	return cloneDefinition(def), ok
}

// List returns all registered Definitions.
func (r *Registry) List() []Definition {
	var list []Definition
	for _, def := range r.agents {
		list = append(list, cloneDefinition(def))
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}

// RegisterAgent adds or replaces a Definition by name.
func (r *Registry) RegisterAgent(def Definition) error {
	if err := def.Validate(); err != nil {
		return fmt.Errorf("agentregistry: invalid definition %q: %w", def.Name, err)
	}
	r.agents[def.Name] = cloneDefinition(def)
	return nil
}

// RegisterTool adds or replaces a tool by toolName.
func (r *Registry) RegisterTool(toolName string, tool toolsetinterface.Tool) error {
	if toolName == "" {
		return errors.New("agentregistry: tool name is required")
	}
	if tool == nil {
		return errors.New("agentregistry: tool is required")
	}
	r.tools[toolName] = tool
	return nil
}

// ValidateTools checks that all agents' references to tools are valid.
func (r *Registry) ValidateTools() error {
	for agentName, def := range r.agents {
		for _, toolName := range def.ToolNames {
			if _, ok := r.tools[toolName]; !ok {
				return fmt.Errorf("agentregistry: agent %q references unknown tool %q", agentName, toolName)
			}
		}
	}
	return nil
}

type BuildOptions struct {
	AgentName   string
	ToolOptions toolsetinterface.Options       // ToolOptions are the effective options used to construct tools after auth/package policy and overrides are applied.
	Request     toolsetinterface.InvokeRequest // Request is the original invocation request.
}

// PreparedAgent contains a fully prepared agent configuration.
//
// It includes resolved tool options, system prompt, tool names, and initial turns, but it does not start a run or send any request messages.
type PreparedAgent struct {
	BuildOptions BuildOptions
	SystemPrompt string
	ToolNames    []string
	InitialTurns []string
	tools        []llmstream.Tool
	created      bool
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

	// AuthPolicyPackage requires a package-scoped authorizer whose code-unit dir matches ToolOptions.GoPkgAbsDir, while preserving the incoming SandboxDir used for
	// relative path resolution. An error occurs if an override is set.
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

func cloneDefinition(def Definition) Definition {
	def.ToolNames = append([]string(nil), def.ToolNames...)
	return def
}

// Validate checks that a Definition is internally consistent.
//
// Validate only checks static definition shape. It does not resolve targets, render prompts, or construct tools.
func (d Definition) Validate() error {
	if d.Name == "" {
		return errors.New("definition name is required")
	}
	if d.AuthPolicy != AuthPolicyDefault && d.AuthPolicy != AuthPolicyPackage {
		return fmt.Errorf("unknown auth policy %q", d.AuthPolicy)
	}
	return nil
}

// Create constructs an idle agent from the prepared configuration.
//
// InitialTurns are applied before the agent is returned. No request messages are sent.
func (p *PreparedAgent) Create(agentCreator agent.AgentCreator) (*agent.Agent, error) {
	if p == nil {
		return nil, errors.New("agentregistry: prepared agent is required")
	}
	if agentCreator == nil {
		return nil, errors.New("agentregistry: AgentCreator is required")
	}
	if p.created {
		return nil, errors.New("agentregistry: prepared agent already created")
	}

	tools := append([]llmstream.Tool(nil), p.tools...)

	var (
		a   *agent.Agent
		err error
	)
	if p.BuildOptions.ToolOptions.Model != "" {
		a, err = agentCreator.New(p.BuildOptions.ToolOptions.Model, p.SystemPrompt, tools)
	} else {
		a, err = agentCreator.NewWithDefaultModel(p.SystemPrompt, tools)
	}
	if err != nil {
		return nil, fmt.Errorf("agentregistry: failed to create agent: %w", err)
	}

	for _, turn := range p.InitialTurns {
		if err := a.AddUserTurn(turn); err != nil {
			return nil, fmt.Errorf("agentregistry: failed to add initial turn: %w", err)
		}
	}

	p.created = true
	return a, nil
}

// Prepare resolves the named agent into a fully prepared configuration without constructing or starting an agent.
func (r *Registry) Prepare(ctx context.Context, agentName string, req toolsetinterface.InvokeRequest) (*PreparedAgent, error) {
	def, ok := r.Lookup(agentName)
	if !ok {
		return nil, fmt.Errorf("agentregistry: unknown agent %q", agentName)
	}

	// 1. Determine effective Authorizer and SandboxDir
	effectiveOpts := req.ToolOptions
	var err error

	switch def.AuthPolicy {
	case AuthPolicyDefault:
		if req.CallerAuthorizer != nil {
			effectiveOpts.Authorizer = req.CallerAuthorizer
		}
		if req.CallerSandboxDir != "" {
			effectiveOpts.SandboxDir = req.CallerSandboxDir
		}
		if req.OverrideAuthorizer != nil {
			effectiveOpts.Authorizer = req.OverrideAuthorizer
		}
		if req.OverrideSandboxDir != "" {
			effectiveOpts.SandboxDir = req.OverrideSandboxDir
		}

	case AuthPolicyPackage:
		if req.OverrideAuthorizer != nil || req.OverrideSandboxDir != "" {
			return nil, errors.New("agentregistry: override authorizer/sandbox dir not allowed with AuthPolicyPackage")
		}
		if req.ToolOptions.GoPkgAbsDir == "" {
			return nil, errors.New("agentregistry: GoPkgAbsDir is required for AuthPolicyPackage")
		}

		if req.CallerSandboxDir != "" {
			effectiveOpts.SandboxDir = req.CallerSandboxDir
		}
		baseAuthorizer := req.CallerAuthorizer
		if baseAuthorizer == nil {
			baseAuthorizer = req.ToolOptions.Authorizer
		}
		if baseAuthorizer == nil {
			return nil, errors.New("agentregistry: authorizer is required for AuthPolicyPackage")
		}

		if !baseAuthorizer.IsCodeUnitDomain() {
			return nil, errors.New("agentregistry: code-unit authorizer is required for AuthPolicyPackage")
		}
		if filepath.Clean(baseAuthorizer.CodeUnitDir()) != filepath.Clean(req.ToolOptions.GoPkgAbsDir) {
			return nil, fmt.Errorf("agentregistry: authorizer code-unit dir %q must equal GoPkgAbsDir %q", baseAuthorizer.CodeUnitDir(), req.ToolOptions.GoPkgAbsDir)
		}

		if effectiveOpts.SandboxDir == "" {
			effectiveOpts.SandboxDir = baseAuthorizer.SandboxDir()
		}

		effectiveOpts.Authorizer = baseAuthorizer
		if effectiveOpts.SandboxDir != "" && filepath.Clean(baseAuthorizer.SandboxDir()) != filepath.Clean(effectiveOpts.SandboxDir) {
			effectiveOpts.Authorizer, err = authdomain.WithUpdatedSandbox(baseAuthorizer, effectiveOpts.SandboxDir)
			if err != nil {
				return nil, fmt.Errorf("agentregistry: failed to update authorizer sandbox: %w", err)
			}
		}

	default:
		return nil, fmt.Errorf("agentregistry: unknown auth policy %q", def.AuthPolicy)
	}

	effectiveOpts.AgentInvoker = r

	buildOpts := BuildOptions{
		AgentName:   agentName,
		ToolOptions: effectiveOpts,
		Request:     req,
	}

	// 2. Build system prompt
	systemPrompt := def.SystemPrompt
	if def.SystemPromptBuilder != nil {
		systemPrompt, err = def.SystemPromptBuilder(buildOpts)
		if err != nil {
			return nil, fmt.Errorf("agentregistry: failed to build system prompt: %w", err)
		}
	}

	// 3. Construct tools
	toolNames := append([]string(nil), def.ToolNames...)
	if def.ToolsBuilder != nil {
		builtToolNames, err := def.ToolsBuilder(effectiveOpts)
		if err != nil {
			return nil, fmt.Errorf("agentregistry: failed to build tool names: %w", err)
		}
		toolNames = append(toolNames, builtToolNames...)
	}

	var tools []llmstream.Tool
	for _, toolName := range toolNames {
		t, ok := r.tools[toolName]
		if !ok {
			return nil, fmt.Errorf("agentregistry: tool %q not registered", toolName)
		}

		builtTool, err := t(effectiveOpts)
		if err != nil {
			return nil, fmt.Errorf("agentregistry: failed to construct tool %q: %w", toolName, err)
		}
		tools = append(tools, builtTool)
	}

	// 4. Build initial turns
	var initialTurns []string
	if def.InitialTurnsBuilder != nil {
		initialTurns, err = def.InitialTurnsBuilder(ctx, buildOpts)
		if err != nil {
			return nil, fmt.Errorf("agentregistry: failed to build initial turns: %w", err)
		}
		initialTurns = append([]string(nil), initialTurns...)
	}

	return &PreparedAgent{
		BuildOptions: buildOpts,
		SystemPrompt: systemPrompt,
		ToolNames:    toolNames,
		InitialTurns: initialTurns,
		tools:        tools,
	}, nil
}

// Create constructs the named agent without starting a run.
//
// The returned agent is idle and already has InitialTurnsBuilder turns applied.
func (r *Registry) Create(ctx context.Context, agentName string, req toolsetinterface.InvokeRequest) (*agent.Agent, error) {
	prepared, err := r.Prepare(ctx, agentName, req)
	if err != nil {
		return nil, err
	}
	return prepared.Create(req.AgentCreator)
}

// Invoke begins executing the named agent, and returns a channel from which to read events.
//
// It preserves invocation-oriented behavior such as sending one empty user message when InvokeRequest.Messages is empty.
func (r *Registry) Invoke(ctx context.Context, agentName string, req toolsetinterface.InvokeRequest) (<-chan agent.Event, error) {
	a, err := r.Create(ctx, agentName, req)
	if err != nil {
		return nil, err
	}

	// Preserve legacy Invoke behavior: an empty Messages slice still sends one empty user message.
	messages := req.Messages
	if len(messages) == 0 {
		messages = []string{""}
	}

	for _, message := range messages[:len(messages)-1] {
		if err := a.AddUserTurn(message); err != nil {
			return nil, fmt.Errorf("agentregistry: failed to add request message: %w", err)
		}
	}

	return a.SendUserMessage(ctx, messages[len(messages)-1]), nil
}
