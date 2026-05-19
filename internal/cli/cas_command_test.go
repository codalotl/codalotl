package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/stretchr/testify/require"
)

func TestRun_CAS_Retrieve_ExistingRecord(t *testing.T) {
	isolateUserConfig(t)
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	pkgDir := filepath.Join(tmp, "p")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "p.go"), []byte("package p\n\nfunc P() {}\n"), 0644))
	t.Setenv(gocas.EnvCASDB, filepath.Join(tmp, "casdb"))
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	storeCASTestRecord(t, tmp, "docs-fix", "p", map[string]string{"result": "ok"})

	{
		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{"codalotl", "cas", "get", "docs-fix", "./p"}, &RunOptions{Out: &out, Err: &errOut})
		require.NoError(t, err)
		require.Equal(t, 0, code)
		require.Empty(t, errOut.String())
		var got map[string]any
		require.NoError(t, json.Unmarshal(out.Bytes(), &got))
		require.Equal(t, true, got["ok"])
		val := got["value"].(map[string]any)
		require.Equal(t, "ok", val["result"])
	}
}

func TestRun_CAS_Retrieve_Miss_PrintsNothingAndExit1(t *testing.T) {
	isolateUserConfig(t)
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	pkgDir := filepath.Join(tmp, "p")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "p.go"), []byte("package p\n\nfunc P() {}\n"), 0644))
	dbRoot := filepath.Join(tmp, "casdb")
	t.Setenv(gocas.EnvCASDB, dbRoot)
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "get", "docs-fix", "./p"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 1, code)
	require.Empty(t, errOut.String())
	require.Empty(t, out.String())
	_, err = os.Stat(dbRoot)
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))
}

func TestRun_CAS_Set_IsNotUserFacing(t *testing.T) {
	isolateUserConfig(t)
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "set", "docs-fix", ".", `"OK"`}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.Empty(t, out.String())
	require.Contains(t, errOut.String(), "unknown subcommand: set")
}

func TestRun_CAS_UnknownNamespace_IsUsageError(t *testing.T) {
	isolateUserConfig(t)
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "get", "unknown-ns", "."}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.Contains(t, errOut.String(), `unknown CAS namespace "unknown-ns"`)
}

func TestRun_CAS_LSNamespaces_ListsRegisteredNamespaces(t *testing.T) {
	isolateUserConfig(t)
	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "ls-namespaces"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Contains(t, lines, "clarify-public-api 1")
	require.Contains(t, lines, "docs-fix 1")
	require.Contains(t, lines, "specconforms 1")
	require.IsIncreasing(t, lines)
}

func TestRun_CAS_LSUnset_ListsPackagesMissingNamespace(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))

	p1Dir := filepath.Join(tmp, "p1")
	require.NoError(t, os.MkdirAll(p1Dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(p1Dir, "p1.go"), []byte("package p1\n\nfunc P1() {}\n"), 0644))

	p2Dir := filepath.Join(tmp, "p2")
	require.NoError(t, os.MkdirAll(p2Dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(p2Dir, "p2.go"), []byte("package p2\n\nfunc P2() {}\n"), 0644))

	t.Setenv(gocas.EnvCASDB, filepath.Join(tmp, "casdb"))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	storeCASTestRecord(t, tmp, "docs-fix", "p1", "OK")

	{
		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{"codalotl", "cas", "ls-unset", "docs-fix"}, &RunOptions{Out: &out, Err: &errOut})
		require.NoError(t, err)
		require.Equal(t, 0, code)
		require.Empty(t, errOut.String())

		got := map[string]bool{}
		for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				got[line] = true
			}
		}

		require.True(t, got["./p2"])
		require.False(t, got["./p1"])
	}
}

func TestRun_CAS_LSSummary_SummarizesCurrentPriorAndMissing(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))

	writePackageFile(t, tmp, "p1", "package p1\n\nfunc P1() {}\n")
	writePackageFile(t, tmp, "p2", "package p2\n\nfunc P2() int { return 1 }\n")
	writePackageFile(t, tmp, "p3", "package p3\n\nfunc P3() {}\n")

	t.Setenv(gocas.EnvCASDB, filepath.Join(tmp, "casdb"))

	storeCASTestRecord(t, tmp, "docs-fix", "p1", "OK")
	storeCASTestRecord(t, tmp, "docs-fix", "p2", "OK")
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p2", "p2.go"), []byte("package p2\n\nfunc P2() int { return 2 }\n"), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "ls-summary", "docs-fix"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Contains(t, out.String(), "Note: Prev CAS '-' with CAS=yes means not applicable (current CAS exists), not false/no previous record.")

	rows := casSummaryRowsByPackage(out.String())
	p1 := requireCASSummaryRow(t, rows, "./p1")
	require.Equal(t, []string{"./p1", "yes", "-", p1[3], "-"}, p1)
	require.NotEqual(t, "-", p1[3])
	p2 := requireCASSummaryRow(t, rows, "./p2")
	require.Equal(t, []string{"./p2", "no", "yes", p2[3], "-"}, p2)
	require.NotEqual(t, "-", p2[3])
	require.Equal(t, []string{"./p3", "no", "no", "-", "-"}, requireCASSummaryRow(t, rows, "./p3"))
}

func TestRun_CAS_LSSummary_CSV(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	writePackageFile(t, tmp, "p", "package p\n\nfunc P() {}\n")
	t.Setenv(gocas.EnvCASDB, filepath.Join(tmp, "casdb"))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "ls-summary", "docs-fix", "--csv"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	records, err := csv.NewReader(strings.NewReader(out.String())).ReadAll()
	require.NoError(t, err)
	require.Equal(t, [][]string{
		{"Package", "CAS", "Prev CAS", "Age", "Churn %"},
		{"./p", "no", "no", "-", "-"},
	}, records)
	require.NotContains(t, out.String(), "Note:")
}

func storeCASTestRecord(t *testing.T, moduleDir string, namespace string, relDir string, value any) {
	t.Helper()

	mod, err := gocode.NewModule(moduleDir)
	require.NoError(t, err)
	pkg, err := mod.LoadPackageByRelativeDir(relDir)
	require.NoError(t, err)
	spec, err := resolveCASNamespaceSpec(namespace)
	require.NoError(t, err)
	db, err := casDBForBaseDir(mod.AbsolutePath)
	require.NoError(t, err)
	require.NoError(t, db.Store(pkg, spec, value))
}

func writePackageFile(t *testing.T, moduleDir string, relDir string, contents string) {
	t.Helper()

	pkgDir := filepath.Join(moduleDir, relDir)
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, filepath.Base(relDir)+".go"), []byte(contents), 0644))
}

func casSummaryRowsByPackage(s string) map[string][]string {
	rows := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 5 || fields[0] == "Package" {
			continue
		}
		rows[fields[0]] = fields
	}
	return rows
}

func requireCASSummaryRow(t *testing.T, rows map[string][]string, pkg string) []string {
	t.Helper()

	row, ok := rows[pkg]
	require.True(t, ok)
	return row
}
