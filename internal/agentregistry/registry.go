package agentregistry

import (
	"context"
	"errors"
	"fmt"
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

// PromptBuilder builds a prompt lazily based on options.
type PromptBuilder func(options BuildOptions) (string, error)

// InitialTurnsBuilder builds user turns that are added before Request.Message is sent.
type InitialTurnsBuilder func(ctx context.Context, options BuildOptions) ([]string, error)

type AuthPackagePolicy string

const (
	// AuthPackagePolicyDefault inherits (directly uses) the authorizer of caller, unless an override is set.
	AuthPackagePolicyDefault AuthPackagePolicy = ""

	// AuthPackagePolicyPackage creates a new Authorizer based on the InvokeRequest's ToolOptions's GoPkgAbsDir. An error occurs if an override is set.
	AuthPackagePolicyPackage AuthPackagePolicy = "package"
)

// Definition is an agent definition.
type Definition struct {
	Name                string        // Name is the stable agent identifier.
	Description         string        // Description is surfaced in llmstream.ToolInfo.
	ToolNames           []string      // List of tools. Refers to tools added to a Registry.
	SystemPrompt        string        // SystemPrompt to use if SystemPromptBuilder is nil.
	SystemPromptBuilder PromptBuilder // SystemPromptBuilder sets and overwrites SystemPrompt if non-nil.

	// InitialTurnsBuilder builds additional user turns before InvokeRequest.Message is sent.
	//
	// This can gather context lazily based on invocation details. Example: package-mode agents may add AGENTS.md, env info, or initial package context.
	InitialTurnsBuilder InitialTurnsBuilder

	// AuthPackagePolicy indicates the policy used for auth AND packages.
	AuthPackagePolicy AuthPackagePolicy
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
	if d.AuthPackagePolicy != AuthPackagePolicyDefault && d.AuthPackagePolicy != AuthPackagePolicyPackage {
		return fmt.Errorf("unknown auth package policy %q", d.AuthPackagePolicy)
	}
	return nil
}

// Invoke begins executing the named agent, and returns a channel from which to read events.
func (r *Registry) Invoke(ctx context.Context, agentName string, req toolsetinterface.InvokeRequest) (<-chan agent.Event, error) {
	if req.AgentCreator == nil {
		return nil, errors.New("agentregistry: AgentCreator is required")
	}

	def, ok := r.Lookup(agentName)
	if !ok {
		return nil, fmt.Errorf("agentregistry: unknown agent %q", agentName)
	}

	// 1. Determine effective Authorizer and SandboxDir
	effectiveOpts := req.ToolOptions
	var err error

	switch def.AuthPackagePolicy {
	case AuthPackagePolicyDefault:
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

	case AuthPackagePolicyPackage:
		if req.OverrideAuthorizer != nil || req.OverrideSandboxDir != "" {
			return nil, errors.New("agentregistry: override authorizer/sandbox dir not allowed with AuthPackagePolicyPackage")
		}
		if req.ToolOptions.GoPkgAbsDir == "" {
			return nil, errors.New("agentregistry: GoPkgAbsDir is required for AuthPackagePolicyPackage")
		}

		effectiveOpts.SandboxDir = req.ToolOptions.GoPkgAbsDir
		baseAuthorizer := req.CallerAuthorizer
		if baseAuthorizer == nil {
			baseAuthorizer = req.ToolOptions.Authorizer
		}
		if baseAuthorizer == nil {
			return nil, errors.New("agentregistry: authorizer is required for AuthPackagePolicyPackage")
		}
		effectiveOpts.Authorizer, err = authdomain.WithUpdatedSandbox(baseAuthorizer, effectiveOpts.SandboxDir)
		if err != nil {
			return nil, fmt.Errorf("agentregistry: failed to update authorizer sandbox: %w", err)
		}

	default:
		return nil, fmt.Errorf("agentregistry: unknown auth package policy %q", def.AuthPackagePolicy)
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
	var tools []llmstream.Tool
	for _, toolName := range def.ToolNames {
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

	// 4. Create the agent
	var a *agent.Agent
	if effectiveOpts.Model != "" {
		a, err = req.AgentCreator.New(effectiveOpts.Model, systemPrompt, tools)
	} else {
		a, err = req.AgentCreator.NewWithDefaultModel(systemPrompt, tools)
	}
	if err != nil {
		return nil, fmt.Errorf("agentregistry: failed to create agent: %w", err)
	}

	// 5. Add initial turns
	if def.InitialTurnsBuilder != nil {
		initialTurns, err := def.InitialTurnsBuilder(ctx, buildOpts)
		if err != nil {
			return nil, fmt.Errorf("agentregistry: failed to build initial turns: %w", err)
		}
		for _, turn := range initialTurns {
			if err := a.AddUserTurn(turn); err != nil {
				return nil, fmt.Errorf("agentregistry: failed to add initial turn: %w", err)
			}
		}
	}

	// 6. Invoke
	return a.SendUserMessage(ctx, req.Message), nil
}
