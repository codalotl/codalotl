package packagemode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/tools/authdomain"

	"github.com/stretchr/testify/require"
)

type stubTool struct {
	name string
}

func (t stubTool) Name() string { return t.name }
func (t stubTool) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{Name: t.name}
}
func (t stubTool) Run(ctx context.Context, params llmstream.ToolCall) llmstream.ToolResult {
	return llmstream.ToolResult{
		CallID: params.CallID,
		Name:   t.name,
		Type:   params.Type,
		Result: "",
	}
}

func TestBuildSystemPrompt_IncludesSkillsPromptAndAuthorizesSkillDirs_WhenShellToolPresent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmp, "home")) // keep SearchPaths deterministic

	sandbox := filepath.Join(tmp, "sandbox")
	require.NoError(t, os.MkdirAll(sandbox, 0o700))
	pkgDir := filepath.Join(sandbox, "p")
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
	a := authdomain.NewCodeUnitAuthorizer(unit, sandboxAuthorizer)
	t.Cleanup(a.Close)

	sysPrompt, err := buildSystemPrompt(sandbox, pkgDir, a, []llmstream.Tool{stubTool{name: "skill_shell"}}, prompt.GoPackageModePromptKindFull)
	require.NoError(t, err)
	require.Contains(t, sysPrompt, "test-skill")
	require.Contains(t, sysPrompt, "test skill description")

	require.NoError(t, a.IsAuthorizedForRead(false, "", "read_file", skillPath))
}

func TestBuildSystemPrompt_DoesNotMentionOrAuthorizeSkills_WhenNoShellToolPresent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmp, "home")) // keep SearchPaths deterministic

	sandbox := filepath.Join(tmp, "sandbox")
	require.NoError(t, os.MkdirAll(sandbox, 0o700))
	pkgDir := filepath.Join(sandbox, "p")
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
	a := authdomain.NewCodeUnitAuthorizer(unit, sandboxAuthorizer)
	t.Cleanup(a.Close)

	sysPrompt, err := buildSystemPrompt(sandbox, pkgDir, a, []llmstream.Tool{stubTool{name: "read_file"}}, prompt.GoPackageModePromptKindFull)
	require.NoError(t, err)
	require.NotContains(t, sysPrompt, "test-skill")

	require.Error(t, a.IsAuthorizedForRead(false, "", "read_file", skillPath))
}
