package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
	qcas "github.com/codalotl/codalotl/internal/q/cas"
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

func TestRun_CAS_Recertify_RequiresNamespaces(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "recertify", "."}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.Empty(t, out.String())
	require.Contains(t, errOut.String(), "missing --namespaces")
}

func TestRun_CAS_Recertify_ValidatesNamespaces(t *testing.T) {
	isolateUserConfig(t)

	for _, tc := range []struct {
		name       string
		namespaces string
		want       string
	}{
		{name: "empty element", namespaces: "docs-fix,,specconforms", want: "empty namespace"},
		{name: "duplicate", namespaces: "docs-fix,docs-fix", want: `duplicate namespace "docs-fix"`},
		{name: "unknown", namespaces: "unknown", want: `unknown CAS namespace "unknown"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			var errOut bytes.Buffer
			code, err := Run([]string{"codalotl", "cas", "recertify", ".", "--namespaces=" + tc.namespaces}, &RunOptions{Out: &out, Err: &errOut})
			require.Error(t, err)
			require.Equal(t, 2, code)
			require.Empty(t, out.String())
			require.Contains(t, errOut.String(), tc.want)
		})
	}
}

func TestRun_CAS_Recertify_CopiesPriorPayloadForward(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	oldContents := "package p\n\nfunc P() int { return 1 }\n"
	newContents := "package p\n\nfunc P() int { return 2 }\n"
	writePackageFile(t, tmp, "p", oldContents)
	t.Setenv(gocas.EnvCASDB, filepath.Join(tmp, "casdb"))

	value := map[string]string{"result": "ok"}
	storeCASTestRecord(t, tmp, "docs-fix", "p", value)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte(newContents), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "recertify", "./p", "--namespaces=docs-fix"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Contains(t, out.String(), "docs-fix: recertified (")

	var got map[string]string
	ok, info := retrieveCASTestRecord(t, tmp, "docs-fix", "p", &got)
	require.True(t, ok)
	require.Equal(t, value, got)
	require.True(t, info.Recertified)
	require.NotEmpty(t, info.RecertifiedFromHash)
	require.Contains(t, info.RecertifiedFromRecord, "docs-fix-1")

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte(oldContents), 0644))
	var prior map[string]string
	ok, priorInfo := retrieveCASTestRecord(t, tmp, "docs-fix", "p", &prior)
	require.True(t, ok)
	require.Equal(t, value, prior)
	require.False(t, priorInfo.Recertified)
}

func TestRun_CAS_Recertify_CurrentRecordIsNoOp(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	writePackageFile(t, tmp, "p", "package p\n\nfunc P() {}\n")
	t.Setenv(gocas.EnvCASDB, filepath.Join(tmp, "casdb"))
	storeCASTestRecord(t, tmp, "docs-fix", "p", "OK")

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "recertify", "./p", "--namespaces=docs-fix"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Contains(t, out.String(), "docs-fix: current (")

	var got string
	ok, info := retrieveCASTestRecord(t, tmp, "docs-fix", "p", &got)
	require.True(t, ok)
	require.Equal(t, "OK", got)
	require.False(t, info.Recertified)
}

func TestRun_CAS_Recertify_NoPriorExitsOne(t *testing.T) {
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
	code, err := Run([]string{"codalotl", "cas", "recertify", "./p", "--namespaces=docs-fix"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 1, code)
	require.Empty(t, errOut.String())
	require.Equal(t, []string{"docs-fix: no prior record"}, cliOutputLines(out.String()))
}

func TestRun_CAS_LSPackages_OutdatedThresholdIncludesMissingAndFiltersFreshPrior(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	createGitRepoMarker(t, tmp)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))

	writePackageFile(t, tmp, "p1", "package p1\n\nfunc P1() {}\n")
	writePackageFile(t, tmp, "p2", "package p2\n\nfunc P2() {}\n")
	writePackageFile(t, tmp, "p3", "package p3\n\nfunc P3() int { return 1 }\n")

	t.Setenv(gocas.EnvCASDB, filepath.Join(tmp, "casdb"))

	storeCASTestRecord(t, tmp, "docs-fix", "p1", "OK")
	storeCASTestRecord(t, tmp, "docs-fix", "p3", "OK")
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p3", "p3.go"), []byte("package p3\n\nfunc P3() int { return 2 }\n"), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "ls-packages", "docs-fix", "--state=outdated", "--min-age=30d", "--min-churn=20"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, []string{"./p2"}, casSummaryPackageList(out.String()))
}

func TestRun_CAS_LSPackages_UsesRepoRootAcrossGoModules(t *testing.T) {
	isolateUserConfig(t)

	repo := t.TempDir()
	createGitRepoMarker(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/repo\n\ngo 1.22\n"), 0644))
	writePackageFile(t, repo, "rootstale", "package rootstale\n\nfunc RootStale() {}\n")

	apiModule := filepath.Join(repo, "services", "api")
	require.NoError(t, os.MkdirAll(apiModule, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(apiModule, "go.mod"), []byte("module example.com/api\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(apiModule, "api.go"), []byte("package api\n\nfunc API() {}\n"), 0644))
	writePackageFile(t, apiModule, "covered", "package covered\n\nfunc Covered() {}\n")

	workerModule := filepath.Join(repo, "services", "worker")
	require.NoError(t, os.MkdirAll(workerModule, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(workerModule, "go.mod"), []byte("module example.com/worker\n\ngo 1.22\n"), 0644))
	writePackageFile(t, workerModule, "job", "package job\n\nfunc Job() {}\n")

	t.Setenv(gocas.EnvCASDB, filepath.Join(repo, "casdb"))
	storeCASTestRecord(t, apiModule, "docs-fix", "covered", "OK")

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(apiModule))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "ls-packages", "docs-fix", "--state=missing"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, []string{
		"./rootstale",
		"./services/api",
		"./services/worker/job",
	}, casSummaryPackageList(out.String()))
}

func TestRun_CAS_LSPackages_SummarizesWorkspaceDiscoveryFromRepoRoot(t *testing.T) {
	isolateUserConfig(t)

	repo := t.TempDir()
	createGitRepoMarker(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/repo\n\ngo 1.22\n"), 0644))
	writePackageFile(t, repo, "rootnotworkspace", "package rootnotworkspace\n\nfunc RootNotWorkspace() {}\n")

	apiModule := filepath.Join(repo, "services", "api")
	require.NoError(t, os.MkdirAll(apiModule, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(apiModule, "go.mod"), []byte("module example.com/api\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(apiModule, "api.go"), []byte("package api\n\nfunc API() {}\n"), 0644))

	workerModule := filepath.Join(repo, "services", "worker")
	require.NoError(t, os.MkdirAll(workerModule, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(workerModule, "go.mod"), []byte("module example.com/worker\n\ngo 1.22\n"), 0644))
	writePackageFile(t, workerModule, "job", "package job\n\nfunc Job() {}\n")

	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.work"), []byte("go 1.22\n\nuse ./services/api\n"), 0644))
	t.Setenv(gocas.EnvCASDB, filepath.Join(repo, "casdb"))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workerModule))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "ls-packages", "docs-fix", "--state=missing"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, []string{"./services/api"}, casSummaryPackageList(out.String()))
}

func TestRun_CAS_LSPackages_IgnoresTestdataModules(t *testing.T) {
	isolateUserConfig(t)

	repo := t.TempDir()
	createGitRepoMarker(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/repo\n\ngo 1.22\n"), 0644))
	writePackageFile(t, repo, "app", "package app\n\nfunc App() {}\n")

	apiModule := filepath.Join(repo, "services", "api")
	require.NoError(t, os.MkdirAll(apiModule, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(apiModule, "go.mod"), []byte("module example.com/api\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(apiModule, "api.go"), []byte("package api\n\nfunc API() {}\n"), 0644))

	fixtureModule := filepath.Join(repo, "testdata", "fixture")
	require.NoError(t, os.MkdirAll(fixtureModule, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(fixtureModule, "go.mod"), []byte("module example.com/fixture\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(fixtureModule, "fixture.go"), []byte("package fixture\n\nfunc Fixture() {}\n"), 0644))

	t.Setenv(gocas.EnvCASDB, filepath.Join(repo, "casdb"))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "ls-packages", "docs-fix", "--state=missing"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, []string{
		"./app",
		"./services/api",
	}, casSummaryPackageList(out.String()))
}

func TestRun_CAS_LSPackages_IgnoresHiddenAndUnderscoreFixtureModules(t *testing.T) {
	isolateUserConfig(t)

	repo := t.TempDir()
	createGitRepoMarker(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/repo\n\ngo 1.22\n"), 0644))
	writePackageFile(t, repo, "app", "package app\n\nfunc App() {}\n")

	for _, fixtureDir := range []string{"._fixtures", "_fixtures"} {
		moduleDir := filepath.Join(repo, fixtureDir)
		require.NoError(t, os.MkdirAll(moduleDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module example.com/fixture\n\ngo 1.22\n"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "fixture.go"), []byte("package fixture\n\nfunc Fixture() {}\n"), 0644))
	}

	t.Setenv(gocas.EnvCASDB, filepath.Join(repo, "casdb"))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "ls-packages", "docs-fix", "--state=missing"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, []string{"./app"}, casSummaryPackageList(out.String()))
}

func TestRun_CAS_LSPackages_AppliesImplicitStaleAgeThreshold(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	createGitRepoMarker(t, tmp)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	writePackageFile(t, tmp, "p", "package p\n\nfunc P() int { return 1 }\n")
	t.Setenv(gocas.EnvCASDB, filepath.Join(tmp, "casdb"))

	storeCASTestRecord(t, tmp, "docs-fix", "p", "OK")
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nfunc P() int { return 2 }\n"), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	{
		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{
			"codalotl", "cas", "ls-packages", "docs-fix",
			"--min-age=0d",
		}, &RunOptions{Out: &out, Err: &errOut})
		require.NoError(t, err)
		require.Equal(t, 0, code)
		require.Empty(t, errOut.String())
		require.Equal(t, []string{"./p"}, casSummaryPackageList(out.String()))
	}

	{
		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{
			"codalotl", "cas", "ls-packages", "docs-fix",
			"--min-age=999d",
		}, &RunOptions{Out: &out, Err: &errOut})
		require.NoError(t, err)
		require.Equal(t, 0, code)
		require.Empty(t, errOut.String())
		require.Empty(t, casSummaryPackageList(out.String()))
	}
}

func TestRun_CAS_LSPackages_ValidatesFilters(t *testing.T) {
	isolateUserConfig(t)

	for _, tc := range []struct {
		name string
		flag string
		want string
	}{
		{name: "state", flag: "--state=nope", want: "invalid --state"},
		{name: "min age", flag: "--min-age=-1d", want: "invalid --min-age"},
		{name: "min churn", flag: "--min-churn=-1", want: "invalid --min-churn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			var errOut bytes.Buffer
			code, err := Run([]string{"codalotl", "cas", "ls-packages", "docs-fix", tc.flag}, &RunOptions{Out: &out, Err: &errOut})
			require.Error(t, err)
			require.Equal(t, 2, code)
			require.Empty(t, out.String())
			require.Contains(t, errOut.String(), tc.want)
		})
	}
}

func TestRun_CAS_Prune_DefaultOutputShape(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	createGitRepoMarker(t, tmp)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	writePackageFile(t, tmp, "p", "package p\n\nfunc P() {}\n")
	t.Setenv(gocas.EnvCASDB, filepath.Join(tmp, "casdb"))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "prune"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, "Deleted CAS records: prior-version=0 superseded=0 total=0\n", out.String())
}

func TestRun_CAS_Prune_ValidatesDays(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "prune", "--days=-1"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.Empty(t, out.String())
	require.Contains(t, errOut.String(), "invalid --days")
}

func TestRun_CAS_Prune_DeletesPriorVersionsAndSupersededOlderThanDays(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	createGitRepoMarker(t, tmp)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	writePackageFile(t, tmp, "p", "package p\n\nfunc P() int { return 1 }\n")
	t.Setenv(gocas.EnvCASDB, filepath.Join(tmp, "casdb"))

	storeCASTestRecord(t, tmp, "docs-fix", "p", "old")
	setCASNamespaceRecordsUnixTimestamp(t, tmp, "docs-fix-1", int(time.Now().Add(-48*time.Hour).Unix()))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nfunc P() int { return 2 }\n"), 0644))
	storeCASTestRecord(t, tmp, "docs-fix", "p", "current")
	priorVersionNamespace := priorVersionCASNamespaceForTest()
	expectedPriorVersionDeletes := 0
	if priorVersionNamespace != "" {
		storePriorVersionCASTestRecord(t, tmp, priorVersionNamespace, "prior-version")
		expectedPriorVersionDeletes = 1
	}

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "prune", "--days=1"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, fmt.Sprintf(
		"Deleted CAS records: prior-version=%d superseded=1 total=%d\n",
		expectedPriorVersionDeletes,
		expectedPriorVersionDeletes+1,
	), out.String())

	var current string
	ok, _ := retrieveCASTestRecord(t, tmp, "docs-fix", "p", &current)
	require.True(t, ok)
	require.Equal(t, "current", current)

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nfunc P() int { return 1 }\n"), 0644))
	var old string
	ok, _ = retrieveCASTestRecord(t, tmp, "docs-fix", "p", &old)
	require.False(t, ok)

	if priorVersionNamespace != "" {
		ok = retrievePriorVersionCASTestRecord(t, tmp, priorVersionNamespace)
		require.False(t, ok)
	}
}

func TestRun_CAS_LSUnset_IsNotUserFacing(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "ls-unset", "docs-fix"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.Empty(t, out.String())
	require.Contains(t, errOut.String(), "unknown subcommand: ls-unset")
}

func TestRun_CAS_LSStale_IsNotUserFacing(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "ls-stale", "docs-fix"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.Empty(t, out.String())
	require.Contains(t, errOut.String(), "unknown subcommand: ls-stale")
}

func TestRun_CAS_LSSummary_IsNotUserFacing(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "ls-summary", "docs-fix"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.Empty(t, out.String())
	require.Contains(t, errOut.String(), "unknown subcommand: ls-summary")
}

func TestRun_CAS_LSPackages_SummarizesCurrentPriorAndMissing(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	createGitRepoMarker(t, tmp)
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
	code, err := Run([]string{"codalotl", "cas", "ls-packages", "docs-fix"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Contains(t, out.String(), "Up to date")
	require.Contains(t, out.String(), "Stale")

	rows := casSummaryRowsByPackage(out.String())
	p1 := requireCASSummaryRow(t, rows, "./p1")
	require.Equal(t, []string{"./p1", "yes", "-", p1[3], "-"}, p1)
	require.NotEqual(t, "-", p1[3])
	p2 := requireCASSummaryRow(t, rows, "./p2")
	require.Equal(t, []string{"./p2", "no", "yes", p2[3], "-"}, p2)
	require.NotEqual(t, "-", p2[3])
	require.Equal(t, []string{"./p3", "no", "no", "-", "-"}, requireCASSummaryRow(t, rows, "./p3"))
}

func TestRun_CAS_LSPackages_FiltersByState(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	createGitRepoMarker(t, tmp)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))

	writePackageFile(t, tmp, "current", "package current\n\nfunc Current() {}\n")
	writePackageFile(t, tmp, "stale", "package stale\n\nfunc Stale() int { return 1 }\n")
	writePackageFile(t, tmp, "missing", "package missing\n\nfunc Missing() {}\n")

	t.Setenv(gocas.EnvCASDB, filepath.Join(tmp, "casdb"))

	storeCASTestRecord(t, tmp, "docs-fix", "current", "OK")
	storeCASTestRecord(t, tmp, "docs-fix", "stale", "OK")
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "stale", "stale.go"), []byte("package stale\n\nfunc Stale() int { return 2 }\n"), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	for _, tc := range []struct {
		state string
		want  []string
	}{
		{state: "current", want: []string{"./current"}},
		{state: "outdated", want: []string{"./missing", "./stale"}},
		{state: "stale", want: []string{"./stale"}},
		{state: "missing", want: []string{"./missing"}},
	} {
		t.Run(tc.state, func(t *testing.T) {
			var out bytes.Buffer
			var errOut bytes.Buffer
			code, err := Run([]string{"codalotl", "cas", "ls-packages", "docs-fix", "--state=" + tc.state}, &RunOptions{Out: &out, Err: &errOut})
			require.NoError(t, err)
			require.Equal(t, 0, code)
			require.Empty(t, errOut.String())
			require.Equal(t, tc.want, casSummaryPackageList(out.String()))
		})
	}
}

func TestRun_CAS_LSPackages_HonorsWorkspaceDiscoveryFromRepoRoot(t *testing.T) {
	isolateUserConfig(t)

	repo := t.TempDir()
	createGitRepoMarker(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/repo\n\ngo 1.22\n"), 0644))
	writePackageFile(t, repo, "rootnotworkspace", "package rootnotworkspace\n\nfunc RootNotWorkspace() {}\n")

	apiModule := filepath.Join(repo, "services", "api")
	require.NoError(t, os.MkdirAll(apiModule, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(apiModule, "go.mod"), []byte("module example.com/api\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(apiModule, "api.go"), []byte("package api\n\nfunc API() {}\n"), 0644))

	workerModule := filepath.Join(repo, "services", "worker")
	require.NoError(t, os.MkdirAll(workerModule, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(workerModule, "go.mod"), []byte("module example.com/worker\n\ngo 1.22\n"), 0644))
	writePackageFile(t, workerModule, "job", "package job\n\nfunc Job() {}\n")

	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.work"), []byte("go 1.22\n\nuse (\n\t./services/api\n\t./services/worker\n)\n"), 0644))
	t.Setenv(gocas.EnvCASDB, filepath.Join(repo, "casdb"))
	storeCASTestRecord(t, apiModule, "docs-fix", ".", "OK")

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workerModule))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "ls-packages", "docs-fix"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	rows := casSummaryRowsByPackage(out.String())
	api := requireCASSummaryRow(t, rows, "./services/api")
	require.Equal(t, []string{"./services/api", "yes", "-", api[3], "-"}, api)
	require.NotEqual(t, "-", api[3])
	require.Equal(t, []string{"./services/worker/job", "no", "no", "-", "-"}, requireCASSummaryRow(t, rows, "./services/worker/job"))
	require.NotContains(t, rows, "./rootnotworkspace")
}

func TestRun_CAS_LSPackages_CSV(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	createGitRepoMarker(t, tmp)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	writePackageFile(t, tmp, "p", "package p\n\nfunc P() {}\n")
	t.Setenv(gocas.EnvCASDB, filepath.Join(tmp, "casdb"))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "ls-packages", "docs-fix", "--csv"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	records, err := csv.NewReader(strings.NewReader(out.String())).ReadAll()
	require.NoError(t, err)
	require.Equal(t, [][]string{
		{"Package", "Up to date", "Stale", "Age", "Churn %"},
		{"./p", "no", "no", "-", "-"},
	}, records)
	require.NotContains(t, out.String(), "Note:")
}

func storeCASTestRecord(t *testing.T, moduleDir string, namespace string, relDir string, value any) {
	t.Helper()

	storeCASTestRecordWithBaseDir(t, moduleDir, moduleDir, namespace, relDir, value)
}

func storeCASTestRecordWithBaseDir(t *testing.T, moduleDir string, dbBaseDir string, namespace string, relDir string, value any) {
	t.Helper()

	mod, err := gocode.NewModule(moduleDir)
	require.NoError(t, err)
	pkg, err := mod.LoadPackageByRelativeDir(relDir)
	require.NoError(t, err)
	spec, err := resolveCASNamespaceSpec(namespace)
	require.NoError(t, err)
	db, err := casDBForBaseDir(dbBaseDir)
	require.NoError(t, err)
	require.NoError(t, db.Store(pkg, spec, value))
}

func retrieveCASTestRecord(t *testing.T, moduleDir string, namespace string, relDir string, target any) (bool, qcas.AdditionalInfo) {
	t.Helper()

	mod, err := gocode.NewModule(moduleDir)
	require.NoError(t, err)
	pkg, err := mod.LoadPackageByRelativeDir(relDir)
	require.NoError(t, err)
	spec, err := resolveCASNamespaceSpec(namespace)
	require.NoError(t, err)
	db, err := casReadDBForBaseDir(moduleDir)
	require.NoError(t, err)
	ok, info, err := db.Retrieve(pkg, spec, target)
	require.NoError(t, err)
	return ok, info
}

func storePriorVersionCASTestRecord(t *testing.T, moduleDir string, namespace string, value any) {
	t.Helper()

	root, err := gocas.RootDirForBaseDir(moduleDir)
	require.NoError(t, err)
	db := qcas.DB{AbsRoot: root}
	require.NoError(t, db.Store(qcas.NewBytesHasher([]byte("prior-version")), namespace, value, nil))
}

func retrievePriorVersionCASTestRecord(t *testing.T, moduleDir string, namespace string) bool {
	t.Helper()

	root, err := gocas.RootDirForBaseDir(moduleDir)
	require.NoError(t, err)
	db := qcas.DB{AbsRoot: root}
	var got string
	ok, _, err := db.Retrieve(qcas.NewBytesHasher([]byte("prior-version")), namespace, &got)
	require.NoError(t, err)
	return ok
}

func priorVersionCASNamespaceForTest() string {
	for _, spec := range sortedCASNamespaceSpecs() {
		if spec.Version > 1 {
			return fmt.Sprintf("%s-%d", spec.Name, spec.Version-1)
		}
	}
	return ""
}

func setCASNamespaceRecordsUnixTimestamp(t *testing.T, moduleDir string, namespace string, unixTimestamp int) {
	t.Helper()

	root, err := gocas.RootDirForBaseDir(moduleDir)
	require.NoError(t, err)
	namespaceDir := filepath.Join(root, namespace)
	var updated int
	require.NoError(t, filepath.WalkDir(namespaceDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var record map[string]any
		if err := json.Unmarshal(b, &record); err != nil {
			return err
		}
		additionalInfo, _ := record["additional_info"].(map[string]any)
		if additionalInfo == nil {
			additionalInfo = map[string]any{}
			record["additional_info"] = additionalInfo
		}
		additionalInfo["unix_timestamp"] = unixTimestamp
		b, err = json.Marshal(record)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, b, 0644); err != nil {
			return err
		}
		updated++
		return nil
	}))
	require.Positive(t, updated)
}

func createGitRepoMarker(t *testing.T, dir string) {
	t.Helper()

	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0755))
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

func casSummaryPackageList(s string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 5 || fields[0] == "Package" {
			continue
		}
		out = append(out, fields[0])
	}
	return out
}

func requireCASSummaryRow(t *testing.T, rows map[string][]string, pkg string) []string {
	t.Helper()

	row, ok := rows[pkg]
	require.True(t, ok)
	return row
}

func cliOutputLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
