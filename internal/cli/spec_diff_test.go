package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun_SpecDiff_Sanity(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))

	pkgDir := filepath.Join(tmp, "p")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "p.go"), []byte("package p\n\nfunc Foo() {}\n"), 0644))

	specPath := filepath.Join(pkgDir, "SPEC.md")
	specBody := "# p\n\n## Public API\n\n```go\nfunc Foo() int {\n\treturn 0\n}\n```\n"
	require.NoError(t, os.WriteFile(specPath, []byte(specBody), 0644))

	for _, arg := range []string{pkgDir, specPath} {
		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{"codalotl", "spec", "diff", arg}, &RunOptions{Out: &out, Err: &errOut})
		require.NoError(t, err)
		require.Equal(t, 0, code)
		require.Empty(t, errOut.String())
		require.Contains(t, out.String(), "Foo")
	}
}
