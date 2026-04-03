package agentbuilder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/agentregistry"
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
	assert.Equal(t, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
		yamlToolVirtualEditFiles,
		coretools.ToolNameShell,
		coretools.ToolNameUpdatePlan,
	}, agentsByName[AgentGeneric].Tools)

	require.Contains(t, agentsByName, AgentPackageModeDefaultContext)
	assert.Equal(t, yamlAgentModePackage, agentsByName[AgentPackageModeDefaultContext].Mode)
	assert.True(t, agentsByName[AgentPackageModeDefaultContext].IncludePackageModeContext)

	require.Contains(t, agentsByName, AgentLimitedPackageMode)
	assert.Equal(t, yamlAgentModePackage, agentsByName[AgentLimitedPackageMode].Mode)
	assert.True(t, agentsByName[AgentLimitedPackageMode].IncludePackageModeContext)
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
		spec: &yamlSubagentSpec{
			Name:    AgentPackageModeDefaultContext,
			Package: "path",
			Message: "{{ .instructions }}",
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

	req, err := tool.buildInvokeRequest("make the change", map[string]any{
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
