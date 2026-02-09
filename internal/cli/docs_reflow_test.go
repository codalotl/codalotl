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

	// Create a tiny module with two packages containing intentionally overlong
	// doc comments so reflow should modify both files.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	pkgDirP := filepath.Join(tmp, "p")
	require.NoError(t, os.MkdirAll(pkgDirP, 0755))
	srcPathP := filepath.Join(pkgDirP, "p.go")
	srcBeforeP := "package p\n\n// Foo does a thing. This is a deliberately long documentation sentence that should be wrapped by the reflow command when the width is small.\nfunc Foo() {}\n"
	require.NoError(t, os.WriteFile(srcPathP, []byte(srcBeforeP), 0644))

	pkgDirQ := filepath.Join(tmp, "q")
	require.NoError(t, os.MkdirAll(pkgDirQ, 0755))
	srcPathQ := filepath.Join(pkgDirQ, "q.go")
	srcBeforeQ := "package q\n\n// Bar does a thing. This is a deliberately long documentation sentence that should be wrapped by the reflow command when the width is small.\nfunc Bar() {}\n"
	require.NoError(t, os.WriteFile(srcPathQ, []byte(srcBeforeQ), 0644))

	// Configure a wide width so config alone would not need to wrap.
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(`{"reflowwidth":200}`+"\n"), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, runErr := Run([]string{"codalotl", "docs", "reflow", "--width=40", pkgDirP, pkgDirQ}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, runErr)
	require.Equal(t, 0, code)
	require.Equal(t, "p/p.go\nq/q.go\n", out.String())
	require.Empty(t, errOut.String())

	afterP, err := os.ReadFile(srcPathP)
	require.NoError(t, err)
	afterQ, err := os.ReadFile(srcPathQ)
	require.NoError(t, err)

	// Sanity-check that the docs were wrapped into multiple lines.
	beforeCountP := strings.Count(srcBeforeP, "\n// ")
	afterCountP := strings.Count(string(afterP), "\n// ")
	require.Equal(t, 1, beforeCountP)
	require.Greater(t, afterCountP, 1)

	beforeCountQ := strings.Count(srcBeforeQ, "\n// ")
	afterCountQ := strings.Count(string(afterQ), "\n// ")
	require.Equal(t, 1, beforeCountQ)
	require.Greater(t, afterCountQ, 1)
}

func TestRun_DocsReflow_Check_DoesNotWrite(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()

	// Create a tiny module with two packages containing intentionally overlong
	// doc comments so reflow would modify both files.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	pkgDirP := filepath.Join(tmp, "p")
	require.NoError(t, os.MkdirAll(pkgDirP, 0755))
	srcPathP := filepath.Join(pkgDirP, "p.go")
	srcBeforeP := "package p\n\n// Foo does a thing. This is a deliberately long documentation sentence that should be wrapped by the reflow command when the width is small.\nfunc Foo() {}\n"
	require.NoError(t, os.WriteFile(srcPathP, []byte(srcBeforeP), 0644))

	pkgDirQ := filepath.Join(tmp, "q")
	require.NoError(t, os.MkdirAll(pkgDirQ, 0755))
	srcPathQ := filepath.Join(pkgDirQ, "q.go")
	srcBeforeQ := "package q\n\n// Bar does a thing. This is a deliberately long documentation sentence that should be wrapped by the reflow command when the width is small.\nfunc Bar() {}\n"
	require.NoError(t, os.WriteFile(srcPathQ, []byte(srcBeforeQ), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, runErr := Run([]string{"codalotl", "docs", "reflow", "--width=40", "--check", pkgDirP, pkgDirQ}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, runErr)
	require.Equal(t, 0, code)
	require.Equal(t, "p/p.go\nq/q.go\n", out.String())
	require.Empty(t, errOut.String())

	afterP, err := os.ReadFile(srcPathP)
	require.NoError(t, err)
	afterQ, err := os.ReadFile(srcPathQ)
	require.NoError(t, err)
	require.Equal(t, srcBeforeP, string(afterP))
	require.Equal(t, srcBeforeQ, string(afterQ))
}
