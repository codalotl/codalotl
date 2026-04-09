package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildPackageInitialContext_ExcludesAgentsMD(t *testing.T) {
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
	require.NotContains(t, out, "agentsmd-test-package")
	require.Contains(t, out, "<current-package>")
}
