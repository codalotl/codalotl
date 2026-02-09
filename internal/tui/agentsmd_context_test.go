package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/stretchr/testify/require"
)

func TestNewSession_NonPackageMode_IncludesAgentsMDContext(t *testing.T) {
	tmp := t.TempDir()
	err := os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte("agentsmd-test-non-package"), 0o600)
	require.NoError(t, err)

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	s, err := newSession(sessionConfig{})
	require.NoError(t, err)
	t.Cleanup(s.Close)

	turns := s.agent.Turns()
	envIdx := indexOfUserTurnContaining(turns, "Here is useful information about the environment")
	require.GreaterOrEqual(t, envIdx, 0)

	agentsIdx := indexOfUserTurnContaining(turns, "agentsmd-test-non-package")
	require.GreaterOrEqual(t, agentsIdx, 0)
	require.Greater(t, agentsIdx, envIdx)
}

func TestBuildPackageInitialContext_PrependsAgentsMDBeforeInitialContext(t *testing.T) {
	tmp := t.TempDir()

	err := os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte("agentsmd-test-package"), 0o600)
	require.NoError(t, err)

	// Minimal module + package so gocode/initialcontext can load it.
	err = os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/agentsmdtest\n\ngo 1.22\n"), 0o600)
	require.NoError(t, err)
	err = os.MkdirAll(filepath.Join(tmp, "p"), 0o700)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nconst X = 1\n"), 0o600)
	require.NoError(t, err)

	out, err := buildPackageInitialContext(tmp, "p", filepath.Join(tmp, "p"), nil)
	require.NoError(t, err)
	require.Contains(t, out, "agentsmd-test-package")

	// initialcontext output is expected to include a <current-package> section; ensure our
	// AGENTS.md text appears before it.
	idxAgents := strings.Index(out, "agentsmd-test-package")
	require.GreaterOrEqual(t, idxAgents, 0)
	idxPkg := strings.Index(out, "<current-package>")
	require.GreaterOrEqual(t, idxPkg, 0)
	require.Less(t, idxAgents, idxPkg)
}

func indexOfUserTurnContaining(turns []llmstream.Turn, needle string) int {
	for i, t := range turns {
		if t.Role != llmstream.RoleUser {
			continue
		}
		if strings.Contains(t.TextContent(), needle) {
			return i
		}
	}
	return -1
}
