package applypatch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// These run the test cases from the codex repo's codex-rs/apply-patch crate, which are described as a "collection of end to end tests for the apply-patch specification,
// meant to be easily portable to other languages or platforms".
func TestApplyPatch_CodexCases(t *testing.T) {
	casesDir := filepath.Join("testdata", "codex_cases")
	caseNames, err := listCodexCaseDirs(casesDir)
	require.NoError(t, err)

	for _, name := range caseNames {
		t.Run(name, func(t *testing.T) {
			_, patchPath, inputDirAbs, expectedDirAbs, err := codexCasePaths(casesDir, name)
			require.NoError(t, err)

			patchBytes, err := os.ReadFile(patchPath)
			require.NoError(t, err)

			td := t.TempDir()
			require.NoError(t, copyTree(td, inputDirAbs))

			// Codex golden cases are filesystem-only: they assert the final state regardless of whether ApplyPatch reported an error.
			_, _ = ApplyPatch(td, string(patchBytes))

			got, err := snapshotDir(td)
			require.NoError(t, err)
			want, err := snapshotDir(expectedDirAbs)
			require.NoError(t, err)

			require.Equal(t, want, got)
		})
	}
}

func listCodexCaseDirs(casesDir string) ([]string, error) {
	entries, err := os.ReadDir(casesDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

func codexCasePaths(casesDir, name string) (base string, patchPath string, inputAbs string, expectedAbs string, err error) {
	base = filepath.Join(casesDir, name)
	patchPath = filepath.Join(base, "patch.txt")

	inputAbs, err = filepath.Abs(filepath.Join(base, "input"))
	if err != nil {
		return "", "", "", "", err
	}
	expectedAbs, err = filepath.Abs(filepath.Join(base, "expected"))
	if err != nil {
		return "", "", "", "", err
	}
	return base, patchPath, inputAbs, expectedAbs, nil
}
