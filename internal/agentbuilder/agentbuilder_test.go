package agentbuilder

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/agentregistry"
	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/q/cmdrunner"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/exttools"
	"github.com/codalotl/codalotl/internal/tools/pkgtools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRegistry_RegistersAgents(t *testing.T) {
	registry, err := BuildRegistry()
	require.NoError(t, err)

	require.NoError(t, registry.ValidateTools())

	require.Len(t, registry.List(), 5)

	genericDef, ok := registry.Lookup(AgentGeneric)
	assert.True(t, ok)
	assert.Equal(t, AgentGeneric, genericDef.Name)

	packageModeDef, ok := registry.Lookup(AgentPackageModeNoContext)
	assert.True(t, ok)
	assert.Equal(t, AgentPackageModeNoContext, packageModeDef.Name)
	assert.Equal(t, agentregistry.AuthPolicyPackage, packageModeDef.AuthPolicy)
	assert.Nil(t, packageModeDef.InitialTurnsBuilder)

	defaultContextDef, ok := registry.Lookup(AgentPackageModeDefaultContext)
	require.True(t, ok)
	assert.Equal(t, AgentPackageModeDefaultContext, defaultContextDef.Name)
	assert.Equal(t, agentregistry.AuthPolicyPackage, defaultContextDef.AuthPolicy)
	assert.NotNil(t, defaultContextDef.InitialTurnsBuilder)

	limitedDef, ok := registry.Lookup(AgentLimitedPackageMode)
	require.True(t, ok)
	assert.Equal(t, AgentLimitedPackageMode, limitedDef.Name)
	assert.Equal(t, agentregistry.AuthPolicyPackage, limitedDef.AuthPolicy)
	assert.NotNil(t, limitedDef.InitialTurnsBuilder)

	clarifyDef, ok := registry.Lookup(agentClarifyPublicAPI)
	require.True(t, ok)
	assert.Equal(t, agentClarifyPublicAPI, clarifyDef.Name)
	assert.Equal(t, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
	}, clarifyDef.ToolNames)
	assert.NotNil(t, clarifyDef.InitialTurnsBuilder)

	clarifyPrompt, err := clarifyDef.SystemPromptBuilder(agentregistry.BuildOptions{})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(clarifyPrompt, prompt.GetBasicPrompt()))
	assert.Contains(t, clarifyPrompt, "read-only agent for clarifying public API documentation")
}

func TestBuildRegistry_InvokeGeneric_OpenAIUsesApplyPatch(t *testing.T) {
	gotPrompt, gotTools := invokeAgentForModel(t, AgentGeneric, llmmodel.ProviderIDOpenAI.DefaultModel())

	assert.Equal(t, prompt.GetBasicPrompt(), gotPrompt)
	assert.Equal(t, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
		coretools.ToolNameApplyPatch,
		coretools.ToolNameShell,
		coretools.ToolNameUpdatePlan,
	}, gotTools)
}

func TestBuildRegistry_InvokeGeneric_NonOpenAIUsesEditWriteDelete(t *testing.T) {
	_, gotTools := invokeAgentForModel(t, AgentGeneric, llmmodel.ProviderIDAnthropic.DefaultModel())

	assert.Equal(t, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
		coretools.ToolNameEdit,
		coretools.ToolNameWrite,
		coretools.ToolNameDelete,
		coretools.ToolNameShell,
		coretools.ToolNameUpdatePlan,
	}, gotTools)
}

func TestBuildRegistry_InvokePackageMode_OpenAIUsesPackagePromptAndTools(t *testing.T) {
	gotPrompt, gotTools := invokeAgentForModel(t, AgentPackageModeNoContext, llmmodel.ProviderIDOpenAI.DefaultModel())

	assert.Equal(t, prompt.GetGoPackageModeModePrompt(prompt.GoPackageModePromptKindFull), gotPrompt)
	assert.Equal(t, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
		coretools.ToolNameApplyPatch,
		coretools.ToolNameSkillShell,
		coretools.ToolNameUpdatePlan,
		exttools.ToolNameDiagnostics,
		exttools.ToolNameFixLints,
		exttools.ToolNameRunTests,
		exttools.ToolNameRunProjectTests,
		pkgtools.ToolNameModuleInfo,
		pkgtools.ToolNameGetPublicAPI,
		pkgtools.ToolNameClarifyPublicAPI,
		pkgtools.ToolNameGetUsage,
		pkgtools.ToolNameUpdateUsage,
		pkgtools.ToolNameChangeAPI,
	}, gotTools)
}

func TestBuildRegistry_InvokePackageMode_NonOpenAIUsesEditWriteDelete(t *testing.T) {
	_, gotTools := invokeAgentForModel(t, AgentPackageModeNoContext, llmmodel.ProviderIDAnthropic.DefaultModel())

	assert.Equal(t, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
		coretools.ToolNameEdit,
		coretools.ToolNameWrite,
		coretools.ToolNameDelete,
		coretools.ToolNameSkillShell,
		coretools.ToolNameUpdatePlan,
		exttools.ToolNameDiagnostics,
		exttools.ToolNameFixLints,
		exttools.ToolNameRunTests,
		exttools.ToolNameRunProjectTests,
		pkgtools.ToolNameModuleInfo,
		pkgtools.ToolNameGetPublicAPI,
		pkgtools.ToolNameClarifyPublicAPI,
		pkgtools.ToolNameGetUsage,
		pkgtools.ToolNameUpdateUsage,
		pkgtools.ToolNameChangeAPI,
	}, gotTools)
}

func TestBuildRegistry_InvokePackageModeDefaultContext_OpenAIUsesPackagePromptAndTools(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	gotPrompt, gotTools := invokeAgentForModel(t, AgentPackageModeDefaultContext, llmmodel.ProviderIDOpenAI.DefaultModel())

	assert.Contains(t, gotPrompt, prompt.GetGoPackageModeModePrompt(prompt.GoPackageModePromptKindFull))
	assert.Contains(t, gotPrompt, "# Skills")
	assert.Equal(t, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
		coretools.ToolNameApplyPatch,
		coretools.ToolNameSkillShell,
		coretools.ToolNameUpdatePlan,
		exttools.ToolNameDiagnostics,
		exttools.ToolNameFixLints,
		exttools.ToolNameRunTests,
		exttools.ToolNameRunProjectTests,
		pkgtools.ToolNameModuleInfo,
		pkgtools.ToolNameGetPublicAPI,
		pkgtools.ToolNameClarifyPublicAPI,
		pkgtools.ToolNameGetUsage,
		pkgtools.ToolNameUpdateUsage,
		pkgtools.ToolNameChangeAPI,
	}, gotTools)
}

func TestBuildRegistry_InvokeLimitedPackageMode_OpenAIUsesLimitedPromptAndTools(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	gotPrompt, gotTools := invokeAgentForModel(t, AgentLimitedPackageMode, llmmodel.ProviderIDOpenAI.DefaultModel())

	assert.Contains(t, gotPrompt, prompt.GetGoPackageModeModePrompt(prompt.GoPackageModePromptKindUpdateUsage))
	assert.Contains(t, gotPrompt, "# Skills")
	assert.Equal(t, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
		coretools.ToolNameApplyPatch,
		coretools.ToolNameSkillShell,
		exttools.ToolNameDiagnostics,
		exttools.ToolNameFixLints,
		exttools.ToolNameRunTests,
		pkgtools.ToolNameGetPublicAPI,
		pkgtools.ToolNameClarifyPublicAPI,
	}, gotTools)
}

func TestBuildRegistry_PackageModeOpenAIApplyPatchRunsPostChecks(t *testing.T) {
	t.Setenv("CODALOTL_AGENTBUILDER_LINTS_HELPER_PROCESS", "1")

	sandbox := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o644))

	pkgDir := filepath.Join(sandbox, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "pkg.go"), []byte("package pkg\n\nfunc F() {}\n"), 0o644))

	steps := []lints.Step{
		{
			ID:    "custom",
			Check: agentbuilderHelperCmd("check", 0),
			Fix:   agentbuilderHelperCmd("custom-fix", 0),
		},
	}

	tools := invokeAgentTools(t, AgentPackageModeNoContext, llmmodel.ProviderIDOpenAI.DefaultModel(), sandbox, pkgDir, steps)
	applyTool := requireTool(t, tools, coretools.ToolNameApplyPatch)

	patch := `*** Begin Patch
*** Update File: pkg/pkg.go
@@
-package pkg
-
-func F() {}
+package pkg
+
+// touch
+func F() {}
*** End Patch`

	result := applyTool.Run(context.Background(), llmstream.ToolCall{
		CallID: "apply-post-checks",
		Name:   coretools.ToolNameApplyPatch,
		Type:   "custom_tool_call",
		Input:  patch,
	})

	require.False(t, result.IsError)
	require.Contains(t, result.Result, "<lint-status")
	require.Contains(t, result.Result, "custom-fix")
}

func TestBuildRegistry_PackageModeNonOpenAIEditAndWriteRunPostChecks(t *testing.T) {
	t.Setenv("CODALOTL_AGENTBUILDER_LINTS_HELPER_PROCESS", "1")

	sandbox := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o644))

	pkgDir := filepath.Join(sandbox, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "pkg.go"), []byte("package pkg\n\nfunc F() {}\n"), 0o644))

	steps := []lints.Step{
		{
			ID:    "custom",
			Check: agentbuilderHelperCmd("check", 0),
			Fix:   agentbuilderHelperCmd("custom-fix", 0),
		},
	}

	tools := invokeAgentTools(t, AgentPackageModeNoContext, llmmodel.ProviderIDAnthropic.DefaultModel(), sandbox, pkgDir, steps)
	editTool := requireTool(t, tools, coretools.ToolNameEdit)
	writeTool := requireTool(t, tools, coretools.ToolNameWrite)

	editResult := editTool.Run(context.Background(), llmstream.ToolCall{
		CallID: "edit-post-checks",
		Name:   coretools.ToolNameEdit,
		Type:   "function_call",
		Input:  `{"path":"pkg/pkg.go","old_text":"func F() {}","new_text":"// touch\nfunc F() {}"}`,
	})
	require.False(t, editResult.IsError)
	require.Contains(t, editResult.Result, "<lint-status")
	require.Contains(t, editResult.Result, "custom-fix")

	content := "package pkg\n\nfunc G() {}\n"
	writeInput, err := json.Marshal(map[string]string{
		"path":    "pkg/extra.go",
		"content": content,
	})
	require.NoError(t, err)
	writeResult := writeTool.Run(context.Background(), llmstream.ToolCall{
		CallID: "write-post-checks",
		Name:   coretools.ToolNameWrite,
		Type:   "function_call",
		Input:  string(writeInput),
	})
	require.False(t, writeResult.IsError)
	require.Contains(t, writeResult.Result, "<lint-status")
	require.Contains(t, writeResult.Result, "custom-fix")
}

func TestBuildRegistry_PackageModeOpenAIApplyPatchUsesDefaultLintStepsWhenUnset(t *testing.T) {
	sandbox := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o644))

	pkgDir := filepath.Join(sandbox, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "pkg.go"), []byte("package pkg\n\nfunc F() {}\n"), 0o644))

	tools := invokeAgentTools(t, AgentPackageModeNoContext, llmmodel.ProviderIDOpenAI.DefaultModel(), sandbox, pkgDir, nil)
	applyTool := requireTool(t, tools, coretools.ToolNameApplyPatch)

	patch := `*** Begin Patch
*** Update File: pkg/pkg.go
@@
-package pkg
-
-func F() {}
+package pkg
+
+func F( ){
+}
*** End Patch`

	result := applyTool.Run(context.Background(), llmstream.ToolCall{
		CallID: "apply-default-post-checks",
		Name:   coretools.ToolNameApplyPatch,
		Type:   "custom_tool_call",
		Input:  patch,
	})

	require.False(t, result.IsError)
	require.Contains(t, result.Result, "<lint-status")
	require.NotContains(t, result.Result, `message="no linters"`)

	content, err := os.ReadFile(filepath.Join(pkgDir, "pkg.go"))
	require.NoError(t, err)
	assert.Equal(t, "package pkg\n\nfunc F() {\n}\n", string(content))
}

func TestBuildRegistry_PackageModeNonOpenAIEditAndWriteUseDefaultLintStepsWhenUnset(t *testing.T) {
	sandbox := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o644))

	pkgDir := filepath.Join(sandbox, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "pkg.go"), []byte("package pkg\n\nfunc F() {}\n"), 0o644))

	tools := invokeAgentTools(t, AgentPackageModeNoContext, llmmodel.ProviderIDAnthropic.DefaultModel(), sandbox, pkgDir, nil)
	editTool := requireTool(t, tools, coretools.ToolNameEdit)
	writeTool := requireTool(t, tools, coretools.ToolNameWrite)

	editResult := editTool.Run(context.Background(), llmstream.ToolCall{
		CallID: "edit-default-post-checks",
		Name:   coretools.ToolNameEdit,
		Type:   "function_call",
		Input:  `{"path":"pkg/pkg.go","old_text":"func F() {}","new_text":"func F( ) {\n}"}`,
	})
	require.False(t, editResult.IsError)
	require.Contains(t, editResult.Result, "<lint-status")
	require.NotContains(t, editResult.Result, `message="no linters"`)

	content, err := os.ReadFile(filepath.Join(pkgDir, "pkg.go"))
	require.NoError(t, err)
	assert.Equal(t, "package pkg\n\nfunc F() {\n}\n", string(content))

	writeContent := "package pkg\n\nfunc G( ) {\n}\n"
	writeInput, err := json.Marshal(map[string]string{
		"path":    "pkg/extra.go",
		"content": writeContent,
	})
	require.NoError(t, err)

	writeResult := writeTool.Run(context.Background(), llmstream.ToolCall{
		CallID: "write-default-post-checks",
		Name:   coretools.ToolNameWrite,
		Type:   "function_call",
		Input:  string(writeInput),
	})
	require.False(t, writeResult.IsError)
	require.Contains(t, writeResult.Result, "<lint-status")
	require.NotContains(t, writeResult.Result, `message="no linters"`)

	content, err = os.ReadFile(filepath.Join(pkgDir, "extra.go"))
	require.NoError(t, err)
	assert.Equal(t, "package pkg\n\nfunc G() {\n}\n", string(content))
}

func TestBuildRegistry_PackageModeChangeAPIUsesFullPackageToolset(t *testing.T) {
	tools := invokeAgentTools(t, AgentPackageModeNoContext, llmmodel.ProviderIDOpenAI.DefaultModel(), "", "", nil)
	changeAPITool := requireTool(t, tools, pkgtools.ToolNameChangeAPI)

	toolsetField := reflect.ValueOf(changeAPITool).Elem().FieldByName("toolset")
	require.True(t, toolsetField.IsValid())
	assert.Equal(t, reflect.ValueOf(packageAgentTools).Pointer(), toolsetField.Pointer())
}

func TestBuildRegistry_PackageModeDefaultContextOpenAIApplyPatchRunsPostChecks(t *testing.T) {
	t.Setenv("CODALOTL_AGENTBUILDER_LINTS_HELPER_PROCESS", "1")
	t.Setenv("HOME", t.TempDir())

	sandbox := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o644))

	pkgDir := filepath.Join(sandbox, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "pkg.go"), []byte("package pkg\n\nfunc F() {}\n"), 0o644))

	steps := []lints.Step{
		{
			ID:    "custom",
			Check: agentbuilderHelperCmd("check", 0),
			Fix:   agentbuilderHelperCmd("custom-fix", 0),
		},
	}

	tools := invokeAgentTools(t, AgentPackageModeDefaultContext, llmmodel.ProviderIDOpenAI.DefaultModel(), sandbox, pkgDir, steps)
	applyTool := requireTool(t, tools, coretools.ToolNameApplyPatch)

	patch := `*** Begin Patch
*** Update File: pkg/pkg.go
@@
-package pkg
-
-func F() {}
+package pkg
+
+// touch
+func F() {}
*** End Patch`

	result := applyTool.Run(context.Background(), llmstream.ToolCall{
		CallID: "apply-post-checks-default-context",
		Name:   coretools.ToolNameApplyPatch,
		Type:   "custom_tool_call",
		Input:  patch,
	})

	require.False(t, result.IsError)
	require.Contains(t, result.Result, "<lint-status")
	require.Contains(t, result.Result, "custom-fix")
}

func TestBuildRegistry_LimitedPackageModeOpenAIApplyPatchRunsPostChecks(t *testing.T) {
	t.Setenv("CODALOTL_AGENTBUILDER_LINTS_HELPER_PROCESS", "1")
	t.Setenv("HOME", t.TempDir())

	sandbox := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o644))

	pkgDir := filepath.Join(sandbox, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "pkg.go"), []byte("package pkg\n\nfunc F() {}\n"), 0o644))

	steps := []lints.Step{
		{
			ID:    "custom",
			Check: agentbuilderHelperCmd("check", 0),
			Fix:   agentbuilderHelperCmd("custom-fix", 0),
		},
	}

	tools := invokeAgentTools(t, AgentLimitedPackageMode, llmmodel.ProviderIDOpenAI.DefaultModel(), sandbox, pkgDir, steps)
	applyTool := requireTool(t, tools, coretools.ToolNameApplyPatch)

	patch := `*** Begin Patch
*** Update File: pkg/pkg.go
@@
-package pkg
-
-func F() {}
+package pkg
+
+// touch
+func F() {}
*** End Patch`

	result := applyTool.Run(context.Background(), llmstream.ToolCall{
		CallID: "apply-post-checks-limited-package",
		Name:   coretools.ToolNameApplyPatch,
		Type:   "custom_tool_call",
		Input:  patch,
	})

	require.False(t, result.IsError)
	require.Contains(t, result.Result, "<lint-status")
	require.Contains(t, result.Result, "custom-fix")
}

func TestBuildRegistry_PackageModeDefaultContextChangeAPIUsesFullPackageToolset(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	tools := invokeAgentTools(t, AgentPackageModeDefaultContext, llmmodel.ProviderIDOpenAI.DefaultModel(), "", "", nil)
	changeAPITool := requireTool(t, tools, pkgtools.ToolNameChangeAPI)

	toolsetField := reflect.ValueOf(changeAPITool).Elem().FieldByName("toolset")
	require.True(t, toolsetField.IsValid())
	assert.Equal(t, reflect.ValueOf(packageAgentTools).Pointer(), toolsetField.Pointer())
}

func TestBuildPackageModeSystemPrompt_IncludesSkillsPromptAndAuthorizesSkillDirs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmp, "home"))

	sandbox := filepath.Join(tmp, "sandbox")
	require.NoError(t, os.MkdirAll(sandbox, 0o700))
	pkgDir := filepath.Join(sandbox, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0o700))

	skillDir := filepath.Join(sandbox, ".codalotl", "skills", "test-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o700))
	skillPath := filepath.Join(skillDir, "SKILL.md")
	require.NoError(t, os.WriteFile(skillPath, []byte(`---
name: test-skill
description: test skill description
---

# Test Skill
`), 0o600))

	sandboxAuthorizer := authdomain.NewAutoApproveAuthorizer(sandbox)
	unit, err := codeunit.NewCodeUnit("test package", pkgDir)
	require.NoError(t, err)
	unit.IncludeEntireSubtree()
	authorizer := authdomain.NewCodeUnitAuthorizer(unit, sandboxAuthorizer)
	t.Cleanup(authorizer.Close)

	gotPrompt, err := buildPackageModeSystemPrompt(agentregistry.BuildOptions{
		ToolOptions: toolsetinterface.Options{
			SandboxDir:  sandbox,
			GoPkgAbsDir: pkgDir,
			Authorizer:  authorizer,
		},
	}, prompt.GoPackageModePromptKindFull)
	require.NoError(t, err)
	require.Contains(t, gotPrompt, "test-skill")
	require.Contains(t, gotPrompt, "test skill description")
	require.NoError(t, authorizer.IsAuthorizedForRead(false, "", "read_file", skillPath))
}

func TestBuildPackageModeDefaultContextInitialTurns_BuildsEnvAndInitialContext(t *testing.T) {
	sandbox := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "go.mod"), []byte("module example.com/test\n\ngo 1.24\n"), 0o644))

	pkgDir := filepath.Join(sandbox, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "pkg.go"), []byte("package pkg\n\n// Hello returns a greeting.\nfunc Hello() string { return \"hello\" }\n"), 0o644))

	turns, err := buildPackageModeDefaultContextInitialTurns(context.Background(), agentregistry.BuildOptions{
		ToolOptions: toolsetinterface.Options{
			SandboxDir:  sandbox,
			GoPkgAbsDir: pkgDir,
		},
	})
	require.NoError(t, err)
	require.Len(t, turns, 2)

	assert.Equal(t, "<env>\nSandbox directory: "+sandbox+"\n</env>", turns[0])
	assert.Contains(t, turns[1], "<current-package>")
	assert.Contains(t, turns[1], `Package relative path: "pkg"`)
	assert.Contains(t, turns[1], "<diagnostics-status ok=\"true\"")
	assert.NotContains(t, turns[1], "deliberately skipped")
}

func TestBuildPackageModeDefaultContextInitialTurns_RootPackageUsesModuleImportPath(t *testing.T) {
	sandbox := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "go.mod"), []byte("module example.com/test\n\ngo 1.24\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "root.go"), []byte("package root\n\nfunc Hello() string { return \"hello\" }\n"), 0o644))

	turns, err := buildPackageModeDefaultContextInitialTurns(context.Background(), agentregistry.BuildOptions{
		ToolOptions: toolsetinterface.Options{
			SandboxDir:  sandbox,
			GoPkgAbsDir: sandbox,
		},
	})
	require.NoError(t, err)
	require.Len(t, turns, 2)

	assert.Contains(t, turns[1], `Package relative path: ""`)
	assert.Contains(t, turns[1], `Package import path: "example.com/test"`)
	assert.NotContains(t, turns[1], `Package import path: "example.com/test/."`)
}

func TestBuildClarifyPublicAPIInitialTurns_GoRequestBuildsEnvAndInitialContext(t *testing.T) {
	sandbox := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "go.mod"), []byte("module example.com/test\n\ngo 1.24\n"), 0o644))

	pkgDir := filepath.Join(sandbox, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "pkg.go"), []byte("package pkg\n\n// Hello returns a greeting.\nfunc Hello() string { return \"hello\" }\n"), 0o644))

	requestBytes, err := json.Marshal(clarifyPublicAPIRequest{
		Path:       "pkg",
		Identifier: "Hello",
		Question:   "What does Hello return?",
	})
	require.NoError(t, err)

	turns, err := buildClarifyPublicAPIInitialTurns(context.Background(), agentregistry.BuildOptions{
		ToolOptions: toolsetinterface.Options{
			SandboxDir: sandbox,
		},
		Request: toolsetinterface.InvokeRequest{
			Messages: []string{string(requestBytes)},
		},
	})
	require.NoError(t, err)
	require.Len(t, turns, 2)

	assert.Equal(t, "<env>\nSandbox directory: "+sandbox+"\n</env>", turns[0])
	assert.Contains(t, turns[1], "Identifier: Hello")
	assert.Contains(t, turns[1], "Path: "+pkgDir)
	assert.Contains(t, turns[1], "<current-package>")
	assert.Contains(t, turns[1], `Package relative path: "pkg"`)
	assert.Contains(t, turns[1], `<diagnostics-status ok="unknown">`)
	assert.Contains(t, turns[1], `(diagnostics not run; deliberately skipped)`)
}

func TestBuildClarifyPublicAPIInitialTurns_RejectsOutsideSandboxPath(t *testing.T) {
	sandbox := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "target.txt")
	require.NoError(t, os.WriteFile(outsidePath, []byte("hello"), 0o644))

	requestBytes, err := json.Marshal(clarifyPublicAPIRequest{
		Path:       outsidePath,
		Identifier: "Hello",
		Question:   "What does Hello mean?",
	})
	require.NoError(t, err)

	_, err = buildClarifyPublicAPIInitialTurns(context.Background(), agentregistry.BuildOptions{
		ToolOptions: toolsetinterface.Options{
			SandboxDir: sandbox,
		},
		Request: toolsetinterface.InvokeRequest{
			Messages: []string{string(requestBytes)},
		},
	})
	require.ErrorContains(t, err, "outside of sandbox")
}

func TestParseClarifyPublicAPIRequest_TextRequest(t *testing.T) {
	request, err := parseClarifyPublicAPIRequest([]string{`Clarify this identifier.

Identifier: Hello
Path: internal/example

Question:
What does Hello do?
Does it allocate?`})
	require.NoError(t, err)

	assert.Equal(t, "Hello", request.Identifier)
	assert.Equal(t, "internal/example", request.Path)
	assert.Equal(t, "What does Hello do?\nDoes it allocate?", request.Question)
}

func invokeAgentForModel(t *testing.T, agentName string, model llmmodel.ModelID) (string, []string) {
	t.Helper()

	gotPrompt, tools := invokeAgentForModelDetailed(t, agentName, model, "", "", nil)
	return gotPrompt, toolNames(tools)
}

func invokeAgentTools(t *testing.T, agentName string, model llmmodel.ModelID, sandbox string, goPkgAbsDir string, lintSteps []lints.Step) []llmstream.Tool {
	t.Helper()

	_, tools := invokeAgentForModelDetailed(t, agentName, model, sandbox, goPkgAbsDir, lintSteps)
	return tools
}

func invokeAgentForModelDetailed(t *testing.T, agentName string, model llmmodel.ModelID, sandbox string, goPkgAbsDir string, lintSteps []lints.Step) (string, []llmstream.Tool) {
	t.Helper()

	registry, err := BuildRegistry()
	require.NoError(t, err)

	if sandbox == "" {
		sandbox = t.TempDir()
	}
	if goPkgAbsDir == "" {
		goPkgAbsDir = sandbox
	}
	if isPackageModeAgent(agentName) {
		ensureGoPackageFixture(t, sandbox, goPkgAbsDir)
	}
	creator := &captureAgentCreator{err: errors.New("stop")}
	authorizer := authdomain.NewAutoApproveAuthorizer(sandbox)

	if isPackageModeAgent(agentName) {
		unit, err := codeunit.NewCodeUnit("package .", goPkgAbsDir)
		require.NoError(t, err)
		authorizer = authdomain.NewCodeUnitAuthorizer(unit, authorizer)
	}

	_, err = registry.Invoke(context.Background(), agentName, toolsetinterface.InvokeRequest{
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

func ensureGoPackageFixture(t *testing.T, sandbox string, goPkgAbsDir string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(goPkgAbsDir, 0o755))

	goModPath := filepath.Join(sandbox, "go.mod")
	if _, err := os.Stat(goModPath); errors.Is(err, os.ErrNotExist) {
		require.NoError(t, os.WriteFile(goModPath, []byte("module example.com/test\n\ngo 1.24\n"), 0o644))
	} else {
		require.NoError(t, err)
	}

	matches, err := filepath.Glob(filepath.Join(goPkgAbsDir, "*.go"))
	require.NoError(t, err)
	if len(matches) == 0 {
		require.NoError(t, os.WriteFile(filepath.Join(goPkgAbsDir, "pkg.go"), []byte("package pkg\n\nfunc F() {}\n"), 0o644))
	}
}

func toolNames(tools []llmstream.Tool) []string {
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name()
	}
	return names
}

type captureAgentCreator struct {
	lastModel        llmmodel.ModelID
	lastSystemPrompt string
	lastTools        []llmstream.Tool
	err              error
}

func (c *captureAgentCreator) New(model llmmodel.ModelID, systemPrompt string, tools []llmstream.Tool) (*agent.Agent, error) {
	c.lastModel = model
	c.lastSystemPrompt = systemPrompt
	c.lastTools = tools
	return nil, c.err
}

func (c *captureAgentCreator) NewWithDefaultModel(systemPrompt string, tools []llmstream.Tool) (*agent.Agent, error) {
	c.lastSystemPrompt = systemPrompt
	c.lastTools = tools
	return nil, c.err
}

var _ agent.AgentCreator = (*captureAgentCreator)(nil)

func requireTool(t *testing.T, tools []llmstream.Tool, name string) llmstream.Tool {
	t.Helper()

	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found in %v", name, toolNames(tools))
	return nil
}

func agentbuilderHelperCmd(stdout string, exitCode int) *cmdrunner.Command {
	return &cmdrunner.Command{
		Command: os.Args[0],
		Args: []string{
			"-test.run=^TestAgentbuilderLintsHelperProcess$",
			"--",
			"stdout=" + stdout,
			"exit=" + strconv.Itoa(exitCode),
		},
		OutcomeFailIfAnyOutput: false,
	}
}

func TestAgentbuilderLintsHelperProcess(t *testing.T) {
	if os.Getenv("CODALOTL_AGENTBUILDER_LINTS_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	delimiter := -1
	for i, a := range args {
		if a == "--" {
			delimiter = i
			break
		}
	}
	if delimiter == -1 {
		os.Exit(2)
	}

	var stdout string
	exitCode := 0

	for _, a := range args[delimiter+1:] {
		if strings.HasPrefix(a, "stdout=") {
			stdout = strings.TrimPrefix(a, "stdout=")
			continue
		}
		if strings.HasPrefix(a, "exit=") {
			n, err := strconv.Atoi(strings.TrimPrefix(a, "exit="))
			if err != nil {
				os.Exit(2)
			}
			exitCode = n
			continue
		}
	}

	if stdout != "" {
		_, _ = os.Stdout.WriteString(stdout)
	}
	os.Exit(exitCode)
}
