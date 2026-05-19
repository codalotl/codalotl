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
	createGitRepoMarker(t, tmp)
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

	storeCASTestRecord(t, tmp, "specconforms", "p1", map[string]bool{"conforms": true})
	storeCASTestRecord(t, tmp, "specconforms", "p2", map[string]bool{"conforms": false})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "spec", "status"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	rows, order := parseSpecStatusRows(out.String())
	require.Equal(t, []string{"./p1", "./p2", "./p3"}, order)

	require.Equal(t, specStatusTestRow{hasSpec: "true", apiMatch: "true", cas: "true"}, rows["./p1"])
	require.Equal(t, specStatusTestRow{hasSpec: "true", apiMatch: "false", cas: "false"}, rows["./p2"])
	require.Equal(t, specStatusTestRow{hasSpec: "false", apiMatch: "-", cas: "unset"}, rows["./p3"])
}

func TestRun_SpecStatus_HonorsWorkspaceDiscoveryFromRepoRoot(t *testing.T) {
	isolateUserConfig(t)

	repo := t.TempDir()
	createGitRepoMarker(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/repo\n\ngo 1.22\n"), 0644))
	writePackageFile(t, repo, "rootnotworkspace", "package rootnotworkspace\n\nfunc RootNotWorkspace() {}\n")

	apiModule := filepath.Join(repo, "services", "api")
	require.NoError(t, os.MkdirAll(apiModule, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(apiModule, "go.mod"), []byte("module example.com/api\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(apiModule, "api.go"), []byte("package api\n\nfunc API() {}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(apiModule, "SPEC.md"), []byte("# api\n\n## Public API\n\n```go\nfunc API() {}\n```\n"), 0644))

	workerModule := filepath.Join(repo, "services", "worker")
	require.NoError(t, os.MkdirAll(workerModule, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(workerModule, "go.mod"), []byte("module example.com/worker\n\ngo 1.22\n"), 0644))
	writePackageFile(t, workerModule, "job", "package job\n\nfunc Job() {}\n")

	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.work"), []byte("go 1.22\n\nuse (\n\t./services/api\n\t./services/worker\n)\n"), 0644))
	t.Setenv("CODALOTL_CAS_DB", filepath.Join(repo, "casdb"))
	storeCASTestRecord(t, apiModule, "specconforms", ".", map[string]bool{"conforms": true})

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workerModule))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "spec", "status"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	rows, order := parseSpecStatusRows(out.String())
	require.Equal(t, []string{"./services/api", "./services/worker/job"}, order)
	require.Equal(t, specStatusTestRow{hasSpec: "true", apiMatch: "true", cas: "true"}, rows["./services/api"])
	require.Equal(t, specStatusTestRow{hasSpec: "false", apiMatch: "-", cas: "unset"}, rows["./services/worker/job"])
	require.NotContains(t, rows, "./rootnotworkspace")
}

type specStatusTestRow struct {
	hasSpec  string
	apiMatch string
	cas      string
}

func parseSpecStatusRows(s string) (map[string]specStatusTestRow, []string) {
	rows := map[string]specStatusTestRow{}
	var order []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 4 {
			continue
		}
		if !strings.HasPrefix(fields[0], ".") {
			continue
		}
		pkg := fields[0]
		rows[pkg] = specStatusTestRow{hasSpec: fields[1], apiMatch: fields[2], cas: fields[3]}
		order = append(order, pkg)
	}
	return rows, order
}
