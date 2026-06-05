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

	fixture := newDocsReflowFixture(t)

	// Configure a wide width so config alone would not need to wrap.
	writeProjectConfig(t, fixture.root, `{"reflowwidth":200}`+"\n")

	chdirForTest(t, fixture.root)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, runErr := Run([]string{"codalotl", "docs", "reflow", "--width=40", fixture.pkgDir("p"), fixture.pkgDir("q")}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, runErr)
	require.Equal(t, 0, code)
	require.Equal(t, "p/p.go\nq/q.go\n", out.String())
	require.Empty(t, errOut.String())

	// Sanity-check that the docs were wrapped into multiple lines.
	for _, pkg := range []string{"p", "q"} {
		before := fixture.source[pkg]
		after := fixture.readSource(t, pkg)
		require.Equal(t, 1, strings.Count(before, "\n// "))
		require.Greater(t, strings.Count(after, "\n// "), 1)
	}
}

func TestRun_DocsReflow_Check_DoesNotWrite(t *testing.T) {
	isolateUserConfig(t)

	fixture := newDocsReflowFixture(t)
	chdirForTest(t, fixture.root)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, runErr := Run([]string{"codalotl", "docs", "reflow", "--width=40", "--check", fixture.pkgDir("p"), fixture.pkgDir("q")}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, runErr)
	require.Equal(t, 0, code)
	require.Equal(t, "p/p.go\nq/q.go\n", out.String())
	require.Empty(t, errOut.String())

	for _, pkg := range []string{"p", "q"} {
		require.Equal(t, fixture.source[pkg], fixture.readSource(t, pkg))
	}
}

type docsReflowFixture struct {
	root   string
	source map[string]string
}

func newDocsReflowFixture(t *testing.T) docsReflowFixture {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))

	fixture := docsReflowFixture{
		root: root,
		source: map[string]string{
			"p": "package p\n\n// Foo does a thing. This is a deliberately long documentation sentence that should be wrapped by the reflow command when the width is small.\nfunc Foo() {}\n",
			"q": "package q\n\n// Bar does a thing. This is a deliberately long documentation sentence that should be wrapped by the reflow command when the width is small.\nfunc Bar() {}\n",
		},
	}
	for pkg, source := range fixture.source {
		pkgDir := fixture.pkgDir(pkg)
		require.NoError(t, os.MkdirAll(pkgDir, 0755))
		require.NoError(t, os.WriteFile(fixture.sourcePath(pkg), []byte(source), 0644))
	}
	return fixture
}

func (f docsReflowFixture) pkgDir(pkg string) string {
	return filepath.Join(f.root, pkg)
}

func (f docsReflowFixture) sourcePath(pkg string) string {
	return filepath.Join(f.pkgDir(pkg), pkg+".go")
}

func (f docsReflowFixture) readSource(t *testing.T, pkg string) string {
	t.Helper()

	b, err := os.ReadFile(f.sourcePath(pkg))
	require.NoError(t, err)
	return string(b)
}
