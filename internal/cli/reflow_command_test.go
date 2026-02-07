package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/updatedocs"
	"github.com/stretchr/testify/require"
)

func TestRun_DocsReflow_PassesConfigWidthToUpdatedocs(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()

	// Create a tiny module with one package.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	pkgDir := filepath.Join(tmp, "p")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "p.go"), []byte("package p\n\n// Foo does a thing.\nfunc Foo() {}\n"), 0644))

	// Configure a non-default reflow width.
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(`{"reflowwidth":77}`+"\n"), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var called bool
	var gotWidth int
	orig := reflowAllDocumentation
	reflowAllDocumentation = func(pkg *gocode.Package, options ...updatedocs.Options) (*gocode.Package, []string, error) {
		called = true
		require.NotNil(t, pkg)
		require.NotEmpty(t, options)
		gotWidth = options[0].ReflowMaxWidth
		return pkg, nil, nil
	}
	t.Cleanup(func() { reflowAllDocumentation = orig })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, runErr := Run([]string{"codalotl", "docs", "reflow", pkgDir}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, runErr)
	require.Equal(t, 0, code)
	require.True(t, called)
	require.Equal(t, 77, gotWidth)
	require.Empty(t, out.String())
	require.Empty(t, errOut.String())
}
