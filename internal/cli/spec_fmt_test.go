package cli

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestRun_SpecFmt_Sanity(t *testing.T) {
	isolateUserConfig(t)
	tmp := t.TempDir()
	pkgDir := filepath.Join(tmp, "p")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	specPath := filepath.Join(pkgDir, "SPEC.md")
	specBefore := "# p\n\n## Public API\n\n```go\nfunc Foo(  a int,b int)int{return a+b}\n```\n"
	require.NoError(t, os.WriteFile(specPath, []byte(specBefore), 0644))
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "spec", "fmt", pkgDir}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, specPath+"\n", out.String())
	after, err := os.ReadFile(specPath)
	require.NoError(t, err)
	require.NotEqual(t, specBefore, string(after))
	require.Contains(t, string(after), "func Foo(a int, b int) int")
	require.Contains(t, string(after), "return a + b")
}
