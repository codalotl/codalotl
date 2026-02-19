package cli

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestRun_SpecLsMismatch_ListsOnlyMismatches(t *testing.T) {
	isolateUserConfig(t)
	tmp := mkdirTempWithRemoveRetry(t, "codalotl-cli-mod-")
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	// p1: mismatch (SPEC signature differs from implementation).
	p1 := filepath.Join(tmp, "p1")
	require.NoError(t, os.MkdirAll(p1, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(p1, "p1.go"), []byte("package p1\n\nfunc Foo() {}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(p1, "SPEC.md"), []byte("# p1\n\n## Public API\n\n```go\nfunc Foo() int {\n\treturn 0\n}\n```\n"), 0644))
	// p2: match (no output expected).
	p2 := filepath.Join(tmp, "p2")
	require.NoError(t, os.MkdirAll(p2, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(p2, "p2.go"), []byte("package p2\n\nfunc Foo() {}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(p2, "SPEC.md"), []byte("# p2\n\n## Public API\n\n```go\nfunc Foo() {}\n```\n"), 0644))
	// p3: missing SPEC.md (no output expected).
	p3 := filepath.Join(tmp, "p3")
	require.NoError(t, os.MkdirAll(p3, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(p3, "p3.go"), []byte("package p3\n\nfunc Foo() {}\n"), 0644))
	// p4: invalid SPEC.md (spec diff errors; should not be listed).
	p4 := filepath.Join(tmp, "p4")
	require.NoError(t, os.MkdirAll(p4, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(p4, "p4.go"), []byte("package p4\n\nfunc Foo() {}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(p4, "SPEC.md"), []byte("# p4\n\n## Public API\n\n```go\nfunc Foo() {}\n"), 0644))
	// p5: broken Go package (go list -e should still include it; spec diff will error; should not be listed).
	p5 := filepath.Join(tmp, "p5")
	require.NoError(t, os.MkdirAll(p5, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(p5, "p5.go"), []byte("package p5\n\nfunc (\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(p5, "SPEC.md"), []byte("# p5\n\n## Public API\n\n```go\nfunc Foo() {}\n```\n"), 0644))
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "spec", "ls-mismatch", "./..."}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, "./p1\n", out.String())
}
func TestRun_SpecLsMismatch_OutputSorted(t *testing.T) {
	isolateUserConfig(t)
	tmp := mkdirTempWithRemoveRetry(t, "codalotl-cli-mod-")
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	for _, name := range []string{"b", "a"} {
		p := filepath.Join(tmp, name)
		require.NoError(t, os.MkdirAll(p, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(p, name+".go"), []byte("package "+name+"\n\nfunc Foo() {}\n"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(p, "SPEC.md"), []byte("# "+name+"\n\n## Public API\n\n```go\nfunc Foo() int {\n\treturn 0\n}\n```\n"), 0644))
	}
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "spec", "ls-mismatch", "./..."}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, "./a\n./b\n", out.String())
}
