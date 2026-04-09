package noninteractive

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildPackageInitialContext_DoesNotReadAgentsMDAutomatically(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte("agentsmd-test-package"), 0o600))

	// Minimal module + package so gocode/initialcontext can load it.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/agentsmdtest\n\ngo 1.22\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "p"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nconst X = 1\n"), 0o600))

	out, err := buildPackageInitialContext(tmp, "p", filepath.Join(tmp, "p"), nil)
	require.NoError(t, err)
	require.NotContains(t, out, "agentsmd-test-package")
	require.Contains(t, out, "<current-package>")
	require.Contains(t, out, "Reminder: all file paths you send to tools")
}

func TestBuildPackageEnvironmentInfo_PreservesEnvironmentAndPackageContextWithoutAgentsMD(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte("agentsmd-test-package"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/agentsmdtest\n\ngo 1.22\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "p"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nconst X = 1\n"), 0o600))

	out := buildPackageEnvironmentInfo(tmp, "p", filepath.Join(tmp, "p"), nil)
	require.Contains(t, out, "<env>")
	require.Contains(t, out, "Sandbox directory:")
	require.Contains(t, out, "<current-package>")
	require.NotContains(t, out, "agentsmd-test-package")
}
