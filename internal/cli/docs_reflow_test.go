package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun_DocsReflow_Sanity(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()

	// Create a tiny module with one package containing an intentionally
	// overlong doc comment so reflow should modify the file.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	pkgDir := filepath.Join(tmp, "p")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))

	srcPath := filepath.Join(pkgDir, "p.go")
	srcBefore := "package p\n\n// Foo does a thing. This is a deliberately long documentation sentence that should be wrapped by the reflow command when the width is small.\nfunc Foo() {}\n"
	require.NoError(t, os.WriteFile(srcPath, []byte(srcBefore), 0644))

	// Configure a wide width so config alone would not need to wrap.
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(`{"reflowwidth":200}`+"\n"), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, runErr := Run([]string{"codalotl", "docs", "reflow", "--width=40", pkgDir}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, runErr)
	require.Equal(t, 0, code)
	require.Equal(t, "p/p.go\n", out.String())
	require.Empty(t, errOut.String())

	after, err := os.ReadFile(srcPath)
	require.NoError(t, err)

	// Sanity-check that the docs were wrapped into multiple lines.
	beforeCount := strings.Count(srcBefore, "\n// ")
	afterCount := strings.Count(string(after), "\n// ")
	require.Equal(t, 1, beforeCount)
	require.Greater(t, afterCount, 1)
}
