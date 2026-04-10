package agentbuilder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/agentregistry"
	"github.com/codalotl/codalotl/internal/agentsmd"
	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedYAMLConfig_DefinesBuiltInAgents(t *testing.T) {
	spec, _, err := loadYAMLRegistrySpecFS(embeddedYAMLData, yamlDefaultConfigPath, yamlDefaultConfigPath)
	require.NoError(t, err)

	agentsByName := make(map[string]yamlAgentSpec, len(spec.Agents))
	for _, agentSpec := range spec.Agents {
		agentsByName[agentSpec.Name] = agentSpec
	}

	require.Contains(t, agentsByName, AgentGeneric)
	assert.Equal(t, yamlAgentModeGeneric, agentsByName[AgentGeneric].Mode)
	assert.Nil(t, agentsByName[AgentGeneric].AgentsMD)
	assert.Equal(t, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
		yamlToolVirtualEditFiles,
		coretools.ToolNameShell,
		coretools.ToolNameUpdatePlan,
	}, agentsByName[AgentGeneric].Tools)

	require.Contains(t, agentsByName, AgentPackageModeDefaultContext)
	assert.Equal(t, yamlAgentModePackage, agentsByName[AgentPackageModeDefaultContext].Mode)
	assert.Nil(t, agentsByName[AgentPackageModeDefaultContext].AgentsMD)
	assert.True(t, agentsByName[AgentPackageModeDefaultContext].IncludePackageModeContext)

	require.Contains(t, agentsByName, AgentLimitedPackageMode)
	assert.Equal(t, yamlAgentModePackage, agentsByName[AgentLimitedPackageMode].Mode)
	assert.True(t, agentsByName[AgentLimitedPackageMode].IncludePackageModeContext)

	require.Contains(t, agentsByName, "pr-review")
	assert.Equal(t, yamlAgentModeGeneric, agentsByName["pr-review"].Mode)
	assert.Equal(t, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
		coretools.ToolNameShell,
	}, agentsByName["pr-review"].Tools)
	require.NotNil(t, agentsByName["pr-review"].Skills)
	assert.False(t, *agentsByName["pr-review"].Skills)
}

func TestAddYAMLToRegistry_AddsAgentsAndTools(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	registry, err := BuildRegistry()
	require.NoError(t, err)

	yamlDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(yamlDir, "extra.md"), []byte("From file prompt.\n"), 0o644))

	yamlPath := filepath.Join(yamlDir, "agents.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(`
agents:
  - name: yaml_generic
    mode: generic
    prompts:
      - name: base
      - file: extra.md
      - text: Tail prompt.
    tools:
      - read_file
      - edit_files
      - shell
    skills: false
    agentsmd: false
  - name: yaml_package
    mode: package
    prompts:
      - name: package-base
      - text: Package tail.
    tools:
      - ls
      - edit_files
      - skill_shell
    include_package_mode_context: true
tools: []
`), 0o644))

	require.NoError(t, AddYAMLToRegistry(registry, yamlPath))

	genericDef, ok := registry.Lookup("yaml_generic")
	require.True(t, ok)
	assert.Nil(t, genericDef.InitialTurnsBuilder)
	assert.Equal(t, "", string(genericDef.AuthPolicy))

	gotPrompt, gotTools := invokeAgentForModelWithRegistryDetailed(t, registry, "yaml_generic", llmmodel.ProviderIDOpenAI.DefaultModel(), "", "", nil)
	assert.Equal(t, joinContextBlocks(prompt.GetBasicPrompt(), "From file prompt.", "Tail prompt."), gotPrompt)
	assert.Equal(t, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameApplyPatch,
		coretools.ToolNameShell,
	}, toolNames(gotTools))

	_, gotTools = invokeAgentForModelWithRegistryDetailed(t, registry, "yaml_generic", llmmodel.ProviderIDAnthropic.DefaultModel(), "", "", nil)
	assert.Equal(t, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameEdit,
		coretools.ToolNameWrite,
		coretools.ToolNameDelete,
		coretools.ToolNameShell,
	}, toolNames(gotTools))

	packageDef, ok := registry.Lookup("yaml_package")
	require.True(t, ok)
	assert.Equal(t, agentregistry.AuthPolicyPackage, packageDef.AuthPolicy)
	require.NotNil(t, packageDef.InitialTurnsBuilder)

	sandbox := t.TempDir()
	pkgDir := filepath.Join(sandbox, "pkg")
	ensureGoPackageFixture(t, sandbox, pkgDir)

	packagePrompt, packageTools := invokeAgentForModelWithRegistryDetailed(t, registry, "yaml_package", llmmodel.ProviderIDOpenAI.DefaultModel(), sandbox, pkgDir, nil)
	assert.Contains(t, packagePrompt, prompt.GetGoPackageModeModePrompt(prompt.GoPackageModePromptKindFull))
	assert.Contains(t, packagePrompt, "Package tail.")
	assert.Contains(t, packagePrompt, "# Skills")
	assert.Equal(t, []string{
		coretools.ToolNameLS,
		coretools.ToolNameApplyPatch,
		coretools.ToolNameSkillShell,
	}, toolNames(packageTools))

	turns, err := packageDef.InitialTurnsBuilder(context.Background(), agentregistry.BuildOptions{
		ToolOptions: toolsetinterface.Options{
			SandboxDir:  sandbox,
			GoPkgAbsDir: pkgDir,
		},
	})
	require.NoError(t, err)
	require.Len(t, turns, 2)
	assert.Equal(t, "<env>\nSandbox directory: "+sandbox+"\n</env>", turns[0])
	assert.Contains(t, turns[1], "<current-package>")
}

func TestAddYAMLToRegistry_AgentsMD_DefaultsEnabledForGenericAgents(t *testing.T) {
	registry, err := BuildRegistry()
	require.NoError(t, err)

	yamlPath := filepath.Join(t.TempDir(), "agents.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(`
agents:
  - name: yaml_generic_agentsmd
    mode: generic
    prompts:
      - text: generic agent
    tools:
      - read_file
    skills: false
tools: []
`), 0o644))

	require.NoError(t, AddYAMLToRegistry(registry, yamlPath))

	sandbox := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "AGENTS.md"), []byte("# Root AGENTS\nroot instructions\n"), 0o644))

	prepared := prepareAgentForModelWithRegistryDetailed(
		t,
		registry,
		"yaml_generic_agentsmd",
		llmmodel.ProviderIDOpenAI.DefaultModel(),
		sandbox,
		"",
		nil,
	)

	want, err := agentsmd.Read(sandbox, sandbox)
	require.NoError(t, err)
	assert.Equal(t, []string{strings.TrimSpace(want)}, prepared.InitialTurns)
}

func TestAddYAMLToRegistry_AgentsMD_ExplicitFalseDisablesInitialTurns(t *testing.T) {
	registry, err := BuildRegistry()
	require.NoError(t, err)

	yamlPath := filepath.Join(t.TempDir(), "agents.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(`
agents:
  - name: yaml_generic_no_agentsmd
    mode: generic
    prompts:
      - text: generic agent
    tools:
      - read_file
    skills: false
    agentsmd: false
tools: []
`), 0o644))

	require.NoError(t, AddYAMLToRegistry(registry, yamlPath))

	def, ok := registry.Lookup("yaml_generic_no_agentsmd")
	require.True(t, ok)
	assert.Nil(t, def.InitialTurnsBuilder)

	sandbox := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "AGENTS.md"), []byte("# Root AGENTS\nroot instructions\n"), 0o644))

	prepared := prepareAgentForModelWithRegistryDetailed(
		t,
		registry,
		"yaml_generic_no_agentsmd",
		llmmodel.ProviderIDOpenAI.DefaultModel(),
		sandbox,
		"",
		nil,
	)
	assert.Empty(t, prepared.InitialTurns)
}

func TestAddYAMLToRegistry_AgentsMD_PackageModeUsesTargetPackageContext(t *testing.T) {
	registry, err := BuildRegistry()
	require.NoError(t, err)

	yamlPath := filepath.Join(t.TempDir(), "agents.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(`
agents:
  - name: yaml_package_agentsmd
    mode: package
    prompts:
      - text: package agent
    tools:
      - read_file
    skills: false
tools: []
`), 0o644))

	require.NoError(t, AddYAMLToRegistry(registry, yamlPath))

	sandbox := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "AGENTS.md"), []byte("# Root AGENTS\nroot instructions\n"), 0o644))
	pkgDir := filepath.Join(sandbox, "pkg")
	ensureGoPackageFixture(t, sandbox, pkgDir)
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "AGENTS.md"), []byte("# Package AGENTS\npackage instructions\n"), 0o644))

	prepared := prepareAgentForModelWithRegistryDetailed(
		t,
		registry,
		"yaml_package_agentsmd",
		llmmodel.ProviderIDOpenAI.DefaultModel(),
		sandbox,
		pkgDir,
		nil,
	)

	want, err := agentsmd.Read(sandbox, pkgDir)
	require.NoError(t, err)
	assert.Equal(t, []string{strings.TrimSpace(want)}, prepared.InitialTurns)
}

func TestAddYAMLToRegistry_AgentsMD_PrecedesPackageModeContext(t *testing.T) {
	registry, err := BuildRegistry()
	require.NoError(t, err)

	yamlPath := filepath.Join(t.TempDir(), "agents.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(`
agents:
  - name: yaml_package_agentsmd_context
    mode: package
    prompts:
      - text: package agent
    tools:
      - read_file
    skills: false
    include_package_mode_context: true
tools: []
`), 0o644))

	require.NoError(t, AddYAMLToRegistry(registry, yamlPath))

	sandbox := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "AGENTS.md"), []byte("# Root AGENTS\nroot instructions\n"), 0o644))
	pkgDir := filepath.Join(sandbox, "pkg")
	ensureGoPackageFixture(t, sandbox, pkgDir)
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "AGENTS.md"), []byte("# Package AGENTS\npackage instructions\n"), 0o644))

	prepared := prepareAgentForModelWithRegistryDetailed(
		t,
		registry,
		"yaml_package_agentsmd_context",
		llmmodel.ProviderIDOpenAI.DefaultModel(),
		sandbox,
		pkgDir,
		nil,
	)

	want, err := agentsmd.Read(sandbox, pkgDir)
	require.NoError(t, err)
	require.Len(t, prepared.InitialTurns, 3)
	assert.Equal(t, "<env>\nSandbox directory: "+sandbox+"\n</env>", prepared.InitialTurns[0])
	assert.Equal(t, strings.TrimSpace(want), prepared.InitialTurns[1])
	assert.Contains(t, prepared.InitialTurns[2], "<current-package>")
	assert.NotContains(t, prepared.InitialTurns[2], "AGENTS.md found at ")
}

func TestAddYAMLToRegistry_CommandToolRunsTemplatedCommand(t *testing.T) {
	registry, err := BuildRegistry()
	require.NoError(t, err)

	yamlPath := filepath.Join(t.TempDir(), "tools.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(`
agents:
  - name: yaml_command_agent
    mode: generic
    prompts:
      - text: command agent
    tools:
      - echo_value
    skills: false
tools:
  - name: echo_value
    description: Echo a value.
    parameters:
      value:
        type: string
        description: Value to echo.
        required: true
    command:
      cmd: sh
      args:
        - -c
        - "printf '%s' '{{ .value }}|{{ .sandbox_dir }}|{{ .package_dir }}'"
`), 0o644))

	require.NoError(t, AddYAMLToRegistry(registry, yamlPath))

	sandbox := t.TempDir()
	_, tools := invokeAgentForModelWithRegistryDetailed(t, registry, "yaml_command_agent", llmmodel.ProviderIDOpenAI.DefaultModel(), sandbox, "", nil)
	tool := requireTool(t, tools, "echo_value")

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "echo-value",
		Name:   "echo_value",
		Type:   "function_call",
		Input:  `{"value":"hello"}`,
	})

	require.False(t, result.IsError)
	assert.Contains(t, result.Result, "hello|"+sandbox+"|")
}

func TestYAMLCommandToolPresenter_UsesReplaceStyleGenericPresentation(t *testing.T) {
	call := llmstream.ToolCall{
		CallID: "echo-call",
		Name:   "echo_value",
		Type:   "function_call",
		Input:  `{"value":"hello"}`,
	}
	result := &llmstream.ToolResult{
		CallID: "echo-call",
		Name:   "echo_value",
		Type:   "function_call",
		Result: "<command>hello</command>",
	}

	tool := &yamlCommandTool{
		info: llmstream.ToolInfo{Name: "echo_value"},
	}

	presentation := tool.Presenter().Present(call, result)
	assert.Equal(t, llmstream.CompletionBehaviorReplace, presentation.Behavior)
	assert.Equal(t, llmstream.NewDefaultToolPresenter().Present(call, result), presentation)
}

func TestYAMLSubagentToolRun_RendersMessagesFromTextFileAndCommand(t *testing.T) {
	registry, err := BuildRegistry()
	require.NoError(t, err)

	yamlDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(yamlDir, "message.md"), []byte("file={{ .value }}|sandbox={{ .sandbox_dir }}|pkg={{ .package_dir }}\n"), 0o644))

	yamlPath := filepath.Join(yamlDir, "messages.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(`
agents:
  - name: yaml_messages_agent
    mode: generic
    prompts:
      - text: message agent
    tools:
      - render_messages
    skills: false
tools:
  - name: render_messages
    description: Render templated subagent messages.
    parameters:
      value:
        type: string
        description: Value to render.
        required: true
    subagent:
      name: generic
      messages:
        - text: "text={{ .value }}|sandbox={{ .sandbox_dir }}|pkg={{ .package_dir }}"
        - file: message.md
        - command:
            cmd: sh
            args:
              - -c
              - "printf 'command=%s|sandbox=%s|pkg=%s' '{{ .value }}' '{{ .sandbox_dir }}' '{{ .package_dir }}'"
`), 0o644))

	require.NoError(t, AddYAMLToRegistry(registry, yamlPath))

	sandbox := t.TempDir()
	pkgDir := filepath.Join(sandbox, "pkg")
	ensureGoPackageFixture(t, sandbox, pkgDir)

	invoker := &captureAgentInvoker{
		events: []agent.Event{
			{
				Type: agent.EventTypeAssistantTurnComplete,
				Turn: &llmstream.Turn{
					Role:  llmstream.RoleAssistant,
					Parts: []llmstream.ContentPart{llmstream.TextContent{Content: "done"}},
				},
			},
			{Type: agent.EventTypeDoneSuccess},
		},
	}

	_, tools := invokeAgentForModelWithRegistryDetailed(
		t,
		registry,
		"yaml_messages_agent",
		llmmodel.ProviderIDOpenAI.DefaultModel(),
		sandbox,
		pkgDir,
		nil,
	)
	tool := requireTool(t, tools, "render_messages")
	require.IsType(t, &yamlSubagentTool{}, tool)
	tool.(*yamlSubagentTool).opts.AgentInvoker = invoker

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "render-messages-call",
		Name:   "render_messages",
		Type:   "function_call",
		Input:  `{"value":"hello"}`,
	})

	require.False(t, result.IsError)
	assert.Equal(t, "done", result.Result)
	assert.Equal(t, "generic", invoker.lastAgentName)
	assert.Equal(t, []string{
		"text=hello|sandbox=" + sandbox + "|pkg=pkg",
		"file=hello|sandbox=" + sandbox + "|pkg=pkg\n",
		"command=hello|sandbox=" + sandbox + "|pkg=pkg",
	}, invoker.lastRequest.Messages)
}

func TestYAMLSubagentToolPresenter_UsesAppendStyleGenericPresentation(t *testing.T) {
	call := llmstream.ToolCall{
		CallID: "review-call",
		Name:   "review",
		Type:   "function_call",
		Input:  `{"base":"main"}`,
	}
	result := &llmstream.ToolResult{
		CallID: "review-call",
		Name:   "review",
		Type:   "function_call",
		Result: `{"ok":true}`,
	}

	tool := &yamlSubagentTool{
		info: llmstream.ToolInfo{Name: "review"},
	}

	presentation := tool.Presenter().Present(call, result)
	assert.Equal(t, llmstream.CompletionBehaviorAppend, presentation.Behavior)
	assert.Equal(t, llmstream.NewAppendToolPresenter().Present(call, result), presentation)
}

func TestAddYAMLToRegistry_RejectsInvalidSubagentMessageConfiguration(t *testing.T) {
	registry, err := BuildRegistry()
	require.NoError(t, err)

	yamlPath := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(`
agents:
  - name: bad_messages
    mode: generic
    prompts:
      - text: bad
    tools:
      - broken
    skills: false
tools:
  - name: broken
    description: broken
    parameters: {}
    subagent:
      name: generic
      message: one
      messages:
        - text: two
`), 0o644))

	err = AddYAMLToRegistry(registry, yamlPath)
	require.ErrorContains(t, err, "exactly one of subagent.message or subagent.messages is required")
}

func TestAddYAMLToRegistry_RejectsInvalidSubagentResultFormat(t *testing.T) {
	registry, err := BuildRegistry()
	require.NoError(t, err)

	yamlPath := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(`
agents:
  - name: bad_result_format
    mode: generic
    prompts:
      - text: bad
    tools:
      - broken
    skills: false
tools:
  - name: broken
    description: broken
    parameters: {}
    subagent:
      name: generic
      message: hi
      result_format: xml
`), 0o644))

	err = AddYAMLToRegistry(registry, yamlPath)
	require.ErrorContains(t, err, `unsupported subagent.result_format "xml"`)
}

func TestYAMLSubagentToolRun_JSONResultHandling(t *testing.T) {
	invoker := &captureAgentInvoker{
		events: []agent.Event{
			{
				Type: agent.EventTypeAssistantTurnComplete,
				Turn: &llmstream.Turn{
					Role:  llmstream.RoleAssistant,
					Parts: []llmstream.ContentPart{llmstream.TextContent{Content: "{\n  \"z\": 1,\n  \"a\": [true, false]\n}"}},
				},
			},
			{Type: agent.EventTypeDoneSuccess},
		},
	}

	tool := &yamlSubagentTool{
		info: llmstream.ToolInfo{Name: "json_review"},
		spec: &yamlNormalizedSubagentSpec{
			Name:         "generic",
			Messages:     []yamlNormalizedSubagentMessageSpec{{Text: "review this"}},
			ResultFormat: yamlResultFormatJSON,
		},
		params: map[string]yamlNormalizedParameter{},
		opts: toolsetinterface.Options{
			AgentInvoker: invoker,
		},
	}

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "json-review-call",
		Name:   "json_review",
		Type:   "function_call",
		Input:  `{}`,
	})

	require.False(t, result.IsError)
	assert.JSONEq(t, `{"a":[true,false],"z":1}`, result.Result)
	assert.Equal(t, []string{"review this"}, invoker.lastRequest.Messages)
}

func TestYAMLSubagentToolRun_InvalidJSONResultReturnsToolError(t *testing.T) {
	tool := &yamlSubagentTool{
		info: llmstream.ToolInfo{Name: "json_review"},
		spec: &yamlNormalizedSubagentSpec{
			Name:         "generic",
			Messages:     []yamlNormalizedSubagentMessageSpec{{Text: "review this"}},
			ResultFormat: yamlResultFormatJSON,
		},
		params: map[string]yamlNormalizedParameter{},
		opts: toolsetinterface.Options{
			AgentInvoker: &captureAgentInvoker{
				events: []agent.Event{
					{
						Type: agent.EventTypeAssistantTurnComplete,
						Turn: &llmstream.Turn{
							Role:  llmstream.RoleAssistant,
							Parts: []llmstream.ContentPart{llmstream.TextContent{Content: "not json"}},
						},
					},
					{Type: agent.EventTypeDoneSuccess},
				},
			},
		},
	}

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "json-review-call",
		Name:   "json_review",
		Type:   "function_call",
		Input:  `{}`,
	})

	require.True(t, result.IsError)
	assert.Contains(t, result.Result, "parse subagent result as json")
}

func TestAddYAMLToRegistry_DuplicateToolDoesNotMutateRegistry(t *testing.T) {
	registry, err := BuildRegistry()
	require.NoError(t, err)

	before := registry.List()
	yamlPath := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(`
agents:
  - name: should_not_be_added
    mode: generic
    prompts:
      - text: hi
    tools:
      - read_file
    skills: false
tools:
  - name: ls
    description: duplicate
    parameters: {}
    command:
      cmd: pwd
`), 0o644))

	err = AddYAMLToRegistry(registry, yamlPath)
	require.ErrorContains(t, err, `tool "ls" already exists`)

	after := registry.List()
	assert.Len(t, after, len(before))
	_, ok := registry.Lookup("should_not_be_added")
	assert.False(t, ok)
}

func TestAddYAMLToRegistry_SkillsRequireShellTool(t *testing.T) {
	registry, err := BuildRegistry()
	require.NoError(t, err)

	yamlPath := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(`
agents:
  - name: missing_shell
    mode: generic
    prompts:
      - text: hi
    tools:
      - read_file
tools: []
`), 0o644))

	err = AddYAMLToRegistry(registry, yamlPath)
	require.ErrorContains(t, err, "skills require shell or skill_shell")
	_, ok := registry.Lookup("missing_shell")
	assert.False(t, ok)
}

func TestLoadYAMLRegistrySpec_RejectsMalformedTrailingDocument(t *testing.T) {
	yamlPath := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte("agents: []\ntools: []\n---\n: bad\n"), 0o644))

	_, _, err := loadYAMLRegistrySpec(yamlPath)
	require.ErrorContains(t, err, "decode yaml file")
}

func TestYAMLSubagentToolRun_PackageModeUsesCallerScopeNotOverrides(t *testing.T) {
	registry, err := BuildRegistry()
	require.NoError(t, err)

	sandbox := t.TempDir()
	targetPkgDir := filepath.Join(sandbox, "targetpkg")
	ensureGoPackageFixture(t, sandbox, targetPkgDir)

	tool := &yamlSubagentTool{
		info: llmstream.ToolInfo{
			Name: "implement",
		},
		spec: &yamlNormalizedSubagentSpec{
			Name:    AgentPackageModeDefaultContext,
			Package: "path",
			Messages: []yamlNormalizedSubagentMessageSpec{
				{Text: "{{ .instructions }}"},
			},
			ResultFormat: yamlResultFormatText,
		},
		params: map[string]yamlNormalizedParameter{
			"path": {
				Type:        "string",
				Description: "Target package.",
				Required:    true,
			},
			"instructions": {
				Type:        "string",
				Description: "Work to perform.",
				Required:    true,
			},
		},
		opts: toolsetinterface.Options{
			AgentName:  "pr-orchestrator",
			Model:      llmmodel.ProviderIDOpenAI.DefaultModel(),
			Authorizer: authdomain.NewAutoApproveAuthorizer(sandbox),
			SandboxDir: sandbox,
		},
		targetPackageMode: true,
	}

	req, err := tool.buildInvokeRequest([]string{"make the change"}, map[string]any{
		"path":         "targetpkg",
		"instructions": "make the change",
	}, &captureAgentCreator{err: errors.New("stop")})
	require.NoError(t, err)

	assert.Equal(t, []string{"make the change"}, req.Messages)
	assert.Empty(t, req.OverrideSandboxDir)
	assert.Nil(t, req.OverrideAuthorizer)
	assert.Equal(t, targetPkgDir, req.ToolOptions.GoPkgAbsDir)
	assert.Equal(t, sandbox, req.CallerSandboxDir)
	assert.Equal(t, sandbox, req.ToolOptions.SandboxDir)

	require.NotNil(t, req.CallerAuthorizer)
	assert.True(t, req.CallerAuthorizer.IsCodeUnitDomain())
	assert.Equal(t, targetPkgDir, req.CallerAuthorizer.CodeUnitDir())
	assert.Equal(t, sandbox, req.CallerAuthorizer.SandboxDir())

	require.NotNil(t, req.ToolOptions.Authorizer)
	assert.True(t, req.ToolOptions.Authorizer.IsCodeUnitDomain())
	assert.Equal(t, targetPkgDir, req.ToolOptions.Authorizer.CodeUnitDir())
	assert.Equal(t, sandbox, req.ToolOptions.Authorizer.SandboxDir())

	_, err = registry.Prepare(context.Background(), AgentPackageModeDefaultContext, req)
	require.NoError(t, err)
}

func TestYAMLSubagentToolBuildTargetPackageAuthorizer_IncludesReachableTestdataOnly(t *testing.T) {
	sandbox := t.TempDir()
	targetPkgDir := filepath.Join(sandbox, "targetpkg")
	ensureGoPackageFixture(t, sandbox, targetPkgDir)

	reachableTestdataFile := filepath.Join(targetPkgDir, "testdata", "fixture.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(reachableTestdataFile), 0o755))
	require.NoError(t, os.WriteFile(reachableTestdataFile, []byte("fixture"), 0o644))

	excludedTestdataFile := filepath.Join(targetPkgDir, "nested", "testdata", "blocked.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(excludedTestdataFile), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(targetPkgDir, "nested", "nested.go"), []byte("package nested\n"), 0o644))
	require.NoError(t, os.WriteFile(excludedTestdataFile, []byte("blocked"), 0o644))

	tool := &yamlSubagentTool{
		opts: toolsetinterface.Options{
			SandboxDir:  sandbox,
			Authorizer:  authdomain.NewAutoApproveAuthorizer(sandbox),
			GoPkgAbsDir: targetPkgDir,
		},
	}

	authorizer := tool.buildTargetPackageAuthorizer(resolvedPackageTarget{
		AbsDir:        targetPkgDir,
		WithinSandbox: true,
	}, sandbox)
	t.Cleanup(authorizer.Close)

	require.NoError(t, authorizer.IsAuthorizedForRead(false, "", "read_file", reachableTestdataFile))
	require.Error(t, authorizer.IsAuthorizedForRead(false, "", "read_file", excludedTestdataFile))
}

func TestBuildRegistry_PROrchestratorImplementTool_InvokesPackageModeSubagent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	sandbox := t.TempDir()
	targetPkgDir := filepath.Join(sandbox, "targetpkg")
	ensureGoPackageFixture(t, sandbox, targetPkgDir)

	invoker := &captureAgentInvoker{
		events: []agent.Event{
			{
				Type: agent.EventTypeAssistantTurnComplete,
				Turn: &llmstream.Turn{
					Role:  llmstream.RoleAssistant,
					Parts: []llmstream.ContentPart{llmstream.TextContent{Content: "implemented target package"}},
				},
			},
			{Type: agent.EventTypeDoneSuccess},
		},
	}

	implementTool := requireTool(t, invokeAgentTools(
		t,
		"pr-orchestrator",
		llmmodel.ProviderIDOpenAI.DefaultModel(),
		sandbox,
		"",
		nil,
	), "implement")
	require.IsType(t, &yamlSubagentTool{}, implementTool)
	implementTool.(*yamlSubagentTool).opts.AgentInvoker = invoker

	result := implementTool.Run(context.Background(), llmstream.ToolCall{
		CallID: "implement-call",
		Name:   "implement",
		Type:   "function_call",
		Input:  `{"path":"targetpkg","instructions":"Add the remaining built-in orchestrator coverage."}`,
	})

	require.False(t, result.IsError)
	assert.Equal(t, "implement-call", result.CallID)
	assert.Equal(t, "implement", result.Name)
	assert.Equal(t, "function_call", result.Type)
	assert.Equal(t, "implemented target package", result.Result)

	assert.Equal(t, AgentPackageModeDefaultContext, invoker.lastAgentName)
	assert.Equal(t, []string{"Add the remaining built-in orchestrator coverage."}, invoker.lastRequest.Messages)
	assert.Equal(t, targetPkgDir, invoker.lastRequest.ToolOptions.GoPkgAbsDir)
	assert.Equal(t, sandbox, invoker.lastRequest.CallerSandboxDir)
	assert.Equal(t, sandbox, invoker.lastRequest.ToolOptions.SandboxDir)
	assert.Empty(t, invoker.lastRequest.OverrideSandboxDir)
	assert.Nil(t, invoker.lastRequest.OverrideAuthorizer)

	require.NotNil(t, invoker.lastRequest.CallerAuthorizer)
	assert.True(t, invoker.lastRequest.CallerAuthorizer.IsCodeUnitDomain())
	assert.Equal(t, targetPkgDir, invoker.lastRequest.CallerAuthorizer.CodeUnitDir())
	assert.Equal(t, sandbox, invoker.lastRequest.CallerAuthorizer.SandboxDir())

	require.NotNil(t, invoker.lastRequest.ToolOptions.Authorizer)
	assert.True(t, invoker.lastRequest.ToolOptions.Authorizer.IsCodeUnitDomain())
	assert.Equal(t, targetPkgDir, invoker.lastRequest.ToolOptions.Authorizer.CodeUnitDir())
	assert.Equal(t, sandbox, invoker.lastRequest.ToolOptions.Authorizer.SandboxDir())
}

func TestBuildRegistry_PROrchestratorImplementTool_GenericModeImportPathResolvesTargetPackage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	registry, err := BuildRegistry()
	require.NoError(t, err)

	sandbox := t.TempDir()
	targetPkgDir := filepath.Join(sandbox, "targetpkg")
	ensureGoPackageFixture(t, sandbox, targetPkgDir)

	invoker := &captureAgentInvoker{
		events: []agent.Event{
			{
				Type: agent.EventTypeAssistantTurnComplete,
				Turn: &llmstream.Turn{
					Role:  llmstream.RoleAssistant,
					Parts: []llmstream.ContentPart{llmstream.TextContent{Content: "implemented target package"}},
				},
			},
			{Type: agent.EventTypeDoneSuccess},
		},
	}

	_, tools := invokeAgentForModelWithRegistryDetailed(
		t,
		registry,
		"pr-orchestrator",
		llmmodel.ProviderIDOpenAI.DefaultModel(),
		sandbox,
		"",
		nil,
	)
	implementTool := requireTool(t, tools, "implement")
	require.IsType(t, &yamlSubagentTool{}, implementTool)

	yamlTool := implementTool.(*yamlSubagentTool)
	assert.Empty(t, yamlTool.opts.GoPkgAbsDir)
	yamlTool.opts.AgentInvoker = invoker

	result := yamlTool.Run(context.Background(), llmstream.ToolCall{
		CallID: "implement-import-call",
		Name:   "implement",
		Type:   "function_call",
		Input:  `{"path":"example.com/test/targetpkg","instructions":"Implement the requested change."}`,
	})

	require.False(t, result.IsError)
	assert.Equal(t, "implemented target package", result.Result)
	assert.Equal(t, AgentPackageModeDefaultContext, invoker.lastAgentName)
	assert.Equal(t, []string{"Implement the requested change."}, invoker.lastRequest.Messages)
	assert.Equal(t, targetPkgDir, invoker.lastRequest.ToolOptions.GoPkgAbsDir)
	assert.Equal(t, sandbox, invoker.lastRequest.CallerSandboxDir)
	assert.Equal(t, sandbox, invoker.lastRequest.ToolOptions.SandboxDir)
}

func TestBuildRegistry_PROrchestratorImplementTool_PrepareSupportsEmptyTargetDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	registry, err := BuildRegistry()
	require.NoError(t, err)

	sandbox := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "go.mod"), []byte("module example.com/test\n\ngo 1.24\n"), 0o644))

	targetPkgDir := filepath.Join(sandbox, "targetpkg")
	require.NoError(t, os.MkdirAll(targetPkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(targetPkgDir, "SPEC.md"), []byte("# targetpkg\n"), 0o644))

	_, tools := invokeAgentForModelWithRegistryDetailed(
		t,
		registry,
		"pr-orchestrator",
		llmmodel.ProviderIDOpenAI.DefaultModel(),
		sandbox,
		"",
		nil,
	)
	implementTool := requireTool(t, tools, "implement")
	require.IsType(t, &yamlSubagentTool{}, implementTool)

	req, err := implementTool.(*yamlSubagentTool).buildInvokeRequest(
		[]string{"Implement the package."},
		map[string]any{
			"path":         "targetpkg",
			"instructions": "Implement the package.",
		},
		nil,
	)
	require.NoError(t, err)

	prepared, err := registry.Prepare(context.Background(), AgentPackageModeDefaultContext, req)
	require.NoError(t, err)
	require.Len(t, prepared.InitialTurns, 2)
	assert.Contains(t, prepared.InitialTurns[1], `Package relative path: "targetpkg"`)
	assert.Contains(t, prepared.InitialTurns[1], "fallback package context; target directory does not currently load as a Go package")
	assert.Contains(t, prepared.InitialTurns[1], "SPEC.md")
}

func TestBuildRegistry_YAMLBackedBuiltInAgentsPreserveToolsets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	tests := []struct {
		name      string
		agentName string
		model     llmmodel.ModelID
		wantTools []string
	}{
		{
			name:      "generic openai",
			agentName: AgentGeneric,
			model:     llmmodel.ProviderIDOpenAI.DefaultModel(),
			wantTools: []string{
				coretools.ToolNameReadFile,
				coretools.ToolNameLS,
				coretools.ToolNameApplyPatch,
				coretools.ToolNameShell,
				coretools.ToolNameUpdatePlan,
			},
		},
		{
			name:      "package default context openai",
			agentName: AgentPackageModeDefaultContext,
			model:     llmmodel.ProviderIDOpenAI.DefaultModel(),
			wantTools: []string{
				coretools.ToolNameReadFile,
				coretools.ToolNameLS,
				coretools.ToolNameApplyPatch,
				coretools.ToolNameSkillShell,
				coretools.ToolNameUpdatePlan,
				"diagnostics",
				"fix_lints",
				"run_tests",
				"run_project_tests",
				"module_info",
				"get_public_api",
				"clarify_public_api",
				"get_usage",
				"update_usage",
				"change_api",
			},
		},
		{
			name:      "limited package openai",
			agentName: AgentLimitedPackageMode,
			model:     llmmodel.ProviderIDOpenAI.DefaultModel(),
			wantTools: []string{
				coretools.ToolNameReadFile,
				coretools.ToolNameLS,
				coretools.ToolNameApplyPatch,
				coretools.ToolNameSkillShell,
				"diagnostics",
				"fix_lints",
				"run_tests",
				"get_public_api",
				"clarify_public_api",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPrompt, gotTools := invokeAgentForModel(t, tt.agentName, tt.model)
			assert.Equal(t, tt.wantTools, gotTools)
			assert.Contains(t, gotPrompt, "# Skills")
		})
	}
}

func invokeAgentForModelWithRegistryDetailed(t *testing.T, registry *agentregistry.Registry, agentName string, model llmmodel.ModelID, sandbox string, goPkgAbsDir string, lintSteps []lints.Step) (string, []llmstream.Tool) {
	t.Helper()

	def, ok := registry.Lookup(agentName)
	require.True(t, ok)

	if sandbox == "" {
		sandbox = t.TempDir()
	}
	if def.AuthPolicy == agentregistry.AuthPolicyPackage && goPkgAbsDir == "" {
		goPkgAbsDir = sandbox
	}
	if def.AuthPolicy == agentregistry.AuthPolicyPackage {
		ensureGoPackageFixture(t, sandbox, goPkgAbsDir)
	}

	creator := &captureAgentCreator{err: errors.New("stop")}
	authorizer := authdomain.NewAutoApproveAuthorizer(sandbox)

	if def.AuthPolicy == agentregistry.AuthPolicyPackage {
		unit, err := codeunit.NewCodeUnit("package .", goPkgAbsDir)
		require.NoError(t, err)
		authorizer = authdomain.NewCodeUnitAuthorizer(unit, authorizer)
	}

	_, err := registry.Invoke(context.Background(), agentName, toolsetinterface.InvokeRequest{
		AgentCreator: creator,
		ToolOptions: toolsetinterface.Options{
			Model:       model,
			Authorizer:  authorizer,
			SandboxDir:  sandbox,
			GoPkgAbsDir: goPkgAbsDir,
			LintSteps:   lintSteps,
		},
	})
	require.ErrorContains(t, err, "stop")

	return creator.lastSystemPrompt, creator.lastTools
}

type captureAgentInvoker struct {
	lastAgentName string
	lastRequest   toolsetinterface.InvokeRequest
	events        []agent.Event
	err           error
}

func (c *captureAgentInvoker) Invoke(ctx context.Context, agentName string, req toolsetinterface.InvokeRequest) (<-chan agent.Event, error) {
	c.lastAgentName = agentName
	c.lastRequest = req

	if c.err != nil {
		return nil, c.err
	}

	events := make(chan agent.Event, len(c.events))
	for _, event := range c.events {
		events <- event
	}
	close(events)
	return events, nil
}

var _ toolsetinterface.AgentInvoker = (*captureAgentInvoker)(nil)
