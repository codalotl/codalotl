package noninteractive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadAgentsMDContextBestEffort_NoAgentsMD_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	require.Empty(t, readAgentsMDContextBestEffort(tmp, tmp))
}

func TestReadAgentsMDContextBestEffort_WithAgentsMD_IncludesContent(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte("agentsmd-test-non-package"), 0o600))

	msg := readAgentsMDContextBestEffort(tmp, tmp)
	require.Contains(t, msg, "agentsmd-test-non-package")
}

func TestBuildPackageInitialContext_PrependsAgentsMDBeforeInitialContext(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte("agentsmd-test-package"), 0o600))

	// Minimal module + package so gocode/initialcontext can load it.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/agentsmdtest\n\ngo 1.22\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "p"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nconst X = 1\n"), 0o600))

	out, err := buildPackageInitialContext(tmp, "p", filepath.Join(tmp, "p"))
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
