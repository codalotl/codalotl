package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun_SpecStatus_PrintsPerPackageStatus(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))

	// p1: has SPEC, matches API, CAS conforms true.
	p1 := filepath.Join(tmp, "p1")
	require.NoError(t, os.MkdirAll(p1, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(p1, "p1.go"), []byte("package p1\n\nfunc Foo() {}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(p1, "SPEC.md"), []byte("# p1\n\n## Public API\n\n```go\nfunc Foo() {}\n```\n"), 0644))

	// p2: has SPEC, mismatches API, CAS conforms false.
	p2 := filepath.Join(tmp, "p2")
	require.NoError(t, os.MkdirAll(p2, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(p2, "p2.go"), []byte("package p2\n\nfunc Foo() {}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(p2, "SPEC.md"), []byte("# p2\n\n## Public API\n\n```go\nfunc Foo() int {\n\treturn 0\n}\n```\n"), 0644))

	// p3: missing SPEC.md, no CAS entry.
	p3 := filepath.Join(tmp, "p3")
	require.NoError(t, os.MkdirAll(p3, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(p3, "p3.go"), []byte("package p3\n\nfunc Foo() {}\n"), 0644))

	t.Setenv("CODALOTL_CAS_DB", filepath.Join(tmp, "casdb"))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	{
		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{"codalotl", "cas", "set", "specconforms-1", "./p1", `{"conforms":true}`}, &RunOptions{Out: &out, Err: &errOut})
		require.NoError(t, err)
		require.Equal(t, 0, code)
		require.Empty(t, errOut.String())
		require.Empty(t, out.String())
	}
	{
		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{"codalotl", "cas", "set", "specconforms-1", "./p2", `{"conforms":false}`}, &RunOptions{Out: &out, Err: &errOut})
		require.NoError(t, err)
		require.Equal(t, 0, code)
		require.Empty(t, errOut.String())
		require.Empty(t, out.String())
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "spec", "status"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	type row struct {
		hasSpec  string
		apiMatch string
		cas      string
	}
	rows := map[string]row{}
	var order []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 4 {
			continue
		}
		if !strings.HasPrefix(fields[0], ".") {
			continue
		}
		pkg := fields[0]
		rows[pkg] = row{hasSpec: fields[1], apiMatch: fields[2], cas: fields[3]}
		order = append(order, pkg)
	}

	require.Equal(t, []string{"./p1", "./p2", "./p3"}, order)

	require.Equal(t, row{hasSpec: "true", apiMatch: "true", cas: "true"}, rows["./p1"])
	require.Equal(t, row{hasSpec: "true", apiMatch: "false", cas: "false"}, rows["./p2"])
	require.Equal(t, row{hasSpec: "false", apiMatch: "-", cas: "unset"}, rows["./p3"])
}
