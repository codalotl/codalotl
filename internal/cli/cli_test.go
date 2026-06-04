package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/docubot"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gopackagediff"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/noninteractive"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
	"github.com/codalotl/codalotl/internal/q/remotemonitor"
	"github.com/stretchr/testify/require"
)

func isolateUserConfig(t *testing.T) {
	t.Helper()
	// Prevent tests from reading developer machine config (ex: ~/.codalotl/config.json).
	t.Setenv("HOME", mkdirTempWithRemoveRetry(t, "codalotl-test-home-"))
	t.Setenv("LOCALAPPDATA", mkdirTempWithRemoveRetry(t, "codalotl-test-localappdata-"))

	// llmmodel key overrides are process-global; ensure tests don't leak state.
	for _, pid := range llmmodel.AllProviderIDs {
		llmmodel.ConfigureProviderKey(pid, "")
		llmmodel.ClearProviderSubscription(pid)
		llmmodel.SetProviderSubscriptionRequired(pid, false)
	}

	// Keep tests hermetic: don't allow developer env vars to satisfy startup validation.
	for _, ev := range llmmodel.ProviderKeyEnvVars() {
		if strings.TrimSpace(ev) != "" {
			t.Setenv(ev, "")
		}
	}

	// Startup validation requires at least one provider key.
	t.Setenv("OPENAI_API_KEY", "sk-test-default")

	// Startup validation also requires a handful of tools. Ensure tests don't
	// depend on whatever happens to be installed on the machine running them.
	ensureToolStubs(t, "gopls", "goimports", "git")

	// Keep tests hermetic: by default, do not allow CLI monitoring/version
	// checks to make outbound network requests.
	origNewMonitor := newCLIMonitor
	newCLIMonitor = func(currentVersion string) *remotemonitor.Monitor {
		return nil
	}
	t.Cleanup(func() { newCLIMonitor = origNewMonitor })
}

func ensureToolStubs(t *testing.T, names ...string) {
	t.Helper()

	binDir := mkdirTempWithRemoveRetry(t, "codalotl-test-bin-")

	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		if runtime.GOOS == "windows" {
			// exec.LookPath honors PATHEXT, so a .bat is sufficient for tests.
			p := filepath.Join(binDir, name+".bat")
			if err := os.WriteFile(p, []byte("@echo off\r\nexit /b 0\r\n"), 0644); err != nil {
				t.Fatalf("write stub %q: %v", p, err)
			}
			continue
		}

		p := filepath.Join(binDir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0644); err != nil {
			t.Fatalf("write stub %q: %v", p, err)
		}
		if err := os.Chmod(p, 0755); err != nil {
			t.Fatalf("chmod stub %q: %v", p, err)
		}
	}

	orig := os.Getenv("PATH")
	if orig == "" {
		t.Setenv("PATH", binDir)
	} else {
		t.Setenv("PATH", binDir+string(os.PathListSeparator)+orig)
	}
}

func restoreOpenAISubscriptionRefreshStub(t *testing.T) {
	t.Helper()

	orig := refreshOpenAIDefaultProviderSubscription
	t.Cleanup(func() { refreshOpenAIDefaultProviderSubscription = orig })
}

func TestRun_Help(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "-h"}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if out.Len() == 0 {
		t.Fatalf("expected help output on stdout")
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got: %q", errOut.String())
	}
}

func TestRun_Help_StaysRootOriented(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "--help"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	got := out.String()
	require.Contains(t, got, "Usage:\n  codalotl [command]\n")
	require.NotContains(t, got, "codalotl [command] [args]")
	require.Contains(t, got, "codalotl docs")
	require.Contains(t, got, "Documentation tools.")
	require.NotContains(t, got, "codalotl docs add")
	require.NotContains(t, got, "codalotl context public")
}

func TestRun_RootUnsupportedArg_IsHelpfulUsageError(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "foo"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.Empty(t, out.String())

	got := errOut.String()
	require.Contains(t, got, "unknown command: foo")
	require.Contains(t, got, "Usage:\n  codalotl [command]\n")
	require.NotContains(t, got, "expected no args")
	require.NotContains(t, got, "codalotl [command] [args]")
}

func TestRun_CommandHelp_IsDetailedAndSkipsStartupValidation(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("PATH", "")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "docs", "add", "--help"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	got := out.String()
	require.Contains(t, got, "codalotl docs add")
	require.Contains(t, got, "Adds missing package documentation comments")
	require.Contains(t, got, "--public-only")
	require.Contains(t, got, "--important")
	require.Contains(t, got, "--include-test")
	require.Contains(t, got, "<path/to/pkg>")
	require.Contains(t, got, "codalotl docs add --public-only internal/mypkg")
	require.Contains(t, got, "codalotl docs add --important internal/mypkg")
}

func TestCommandMetadata_ToolFacingCommands(t *testing.T) {
	root, _ := newRootCommand(false)

	for _, names := range [][]string{
		{"context", "public"},
		{"context", "initial"},
		{"context", "packages"},
		{"docs", "add"},
		{"docs", "fix"},
		{"docs", "status"},
		{"docs", "reflow"},
		{"auth", "openai", "login"},
		{"auth", "openai", "logout"},
		{"auth", "openai", "status"},
		{"pr", "new"},
		{"pr", "refactor"},
		{"spec", "fmt"},
		{"spec", "diff"},
		{"spec", "ls-mismatch"},
		{"spec", "status"},
		{"cas", "get"},
		{"cas", "ls-namespaces"},
		{"cas", "ls-stale"},
		{"cas", "prune"},
		{"cas", "recertify"},
	} {
		cmd := requireCommand(t, root, names...)
		require.NotEmpty(t, cmd.Short)
		require.NotEmpty(t, cmd.Long)
		require.NotEmpty(t, cmd.Example)
		switch strings.Join(names, " ") {
		case "context packages", "docs status", "auth openai logout", "auth openai status", "spec status", "cas ls-namespaces", "cas prune":
		default:
			require.NotEmpty(t, cmd.Usage)
		}
	}
}

func TestRun_CAS_LSStale_HelpIncludesThresholdFlags(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("PATH", "")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "ls-stale", "--help"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	got := out.String()
	require.Contains(t, got, "codalotl cas ls-stale")
	require.Contains(t, got, "--stale-after-days")
	require.Contains(t, got, "--min-churn-percent")
	require.Contains(t, got, "codalotl cas ls-stale specconforms")
}

func TestRun_CAS_Prune_HelpIncludesDaysFlag(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("PATH", "")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "cas", "prune", "--help"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	got := out.String()
	require.Contains(t, got, "codalotl cas prune")
	require.Contains(t, got, "--days")
	require.Contains(t, got, "codalotl cas prune --days=14")
}

func TestHelpMetadata_LeafCatalogIncludesExecutableLeaves(t *testing.T) {
	root, _ := newRootCommand(false)

	var out bytes.Buffer
	qcli.WriteHelp(&out, root, root, qcli.HelpOptions{LeafCommands: true})

	got := out.String()
	require.Contains(t, got, "codalotl auth openai login")
	require.Contains(t, got, "codalotl auth openai logout")
	require.Contains(t, got, "codalotl auth openai status")
	require.Contains(t, got, "codalotl docs add")
	require.Contains(t, got, "codalotl docs fix")
	require.Contains(t, got, "codalotl docs status")
	require.NotContains(t, got, "codalotl docs improve-from-clarify")
	require.Contains(t, got, "codalotl context public")
	require.Contains(t, got, "codalotl spec diff")
	require.Contains(t, got, "Add missing documentation comments to a package.")
	require.NotContains(t, got, "codalotl docs\n")
}

func TestRun_Help_IgnoresStartupValidation(t *testing.T) {
	isolateUserConfig(t)

	// Deliberately break startup requirements; help should still succeed.
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("PATH", "")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "-h"}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if out.Len() == 0 {
		t.Fatalf("expected help output on stdout")
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got: %q", errOut.String())
	}
}

func TestRun_Exec_AcceptsModelFlag(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "exec", "--model", "anything"}, &RunOptions{Out: &out, Err: &errOut})
	if err == nil {
		t.Fatalf("expected non-nil error")
	}
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d (err=%v)", code, err)
	}
	if strings.Contains(strings.ToLower(errOut.String()), "unknown flag") {
		t.Fatalf("expected --model to be accepted, got stderr: %q", errOut.String())
	}
}

func TestRun_Exec_JSONFlagSetsOutputJSON(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	origRunNoninteractiveExec := runNoninteractiveExec
	t.Cleanup(func() { runNoninteractiveExec = origRunNoninteractiveExec })

	var gotPrompt string
	var gotOpts noninteractive.Options
	runNoninteractiveExec = func(userPrompt string, opts noninteractive.Options) error {
		gotPrompt = userPrompt
		gotOpts = opts
		_, err := opts.Out.Write([]byte("{\"event\":\"done\"}\n"))
		require.NoError(t, err)
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "exec", "--json", "hello world"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, "hello world", gotPrompt)
	require.True(t, gotOpts.OutputJSON)
	require.Equal(t, "{\"event\":\"done\"}\n", out.String())
}

func TestRun_Version_PrintsVersion(t *testing.T) {
	isolateUserConfig(t)

	orig := Version
	Version = "9.9.9-test"
	t.Cleanup(func() { Version = orig })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "version"}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got: %q", errOut.String())
	}
	if got := out.String(); got != "9.9.9-test\n" {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

func TestRun_Version_IgnoresStartupValidation(t *testing.T) {
	isolateUserConfig(t)

	// Deliberately break startup requirements; version should still succeed.
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("PATH", "")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "version"}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got: %q", errOut.String())
	}
}

func TestRun_Version_ExtraArg_IsUsageError(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "version", "nope"}, &RunOptions{Out: &out, Err: &errOut})
	if err == nil {
		t.Fatalf("expected non-nil error")
	}
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d (err=%v)", code, err)
	}
	if errOut.Len() == 0 {
		t.Fatalf("expected stderr output for usage error")
	}
}

func TestRun_ContextPublic_MissingArg_IsUsageError(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "context", "public"}, &RunOptions{Out: &out, Err: &errOut})
	if err == nil {
		t.Fatalf("expected non-nil error")
	}
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d (err=%v)", code, err)
	}
	if errOut.Len() == 0 {
		t.Fatalf("expected stderr output for usage error")
	}
}

func TestRun_ContextPublic_WritesDocs(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()

	// Create a tiny module with one package.
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	pkgDir := filepath.Join(tmp, "p")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("mkdir pkg: %v", err)
	}
	src := `package p

// Foo does a thing.
func Foo() {}
`
	if err := os.WriteFile(filepath.Join(pkgDir, "p.go"), []byte(src), 0644); err != nil {
		t.Fatalf("write p.go: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "context", "public", pkgDir}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", errOut.String())
	}
	if !strings.Contains(out.String(), "Foo") {
		t.Fatalf("expected docs to mention Foo, got:\n%s", out.String())
	}
}

func TestRun_ContextPackages_WritesList(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()

	// Create a tiny module with two packages.
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	pkgPDir := filepath.Join(tmp, "p")
	if err := os.MkdirAll(pkgPDir, 0755); err != nil {
		t.Fatalf("mkdir p: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgPDir, "p.go"), []byte("package p\n\nfunc P() {}\n"), 0644); err != nil {
		t.Fatalf("write p.go: %v", err)
	}
	pkgQDir := filepath.Join(tmp, "q")
	if err := os.MkdirAll(pkgQDir, 0755); err != nil {
		t.Fatalf("mkdir q: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgQDir, "q.go"), []byte("package q\n\nfunc Q() {}\n"), 0644); err != nil {
		t.Fatalf("write q.go: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "context", "packages"}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", errOut.String())
	}
	got := out.String()
	if !strings.Contains(got, "example.com/tmpmod/p") {
		t.Fatalf("expected output to include package p, got:\n%s", got)
	}
	if !strings.Contains(got, "example.com/tmpmod/q") {
		t.Fatalf("expected output to include package q, got:\n%s", got)
	}
}

func TestRun_ContextPackages_SearchFilters(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()

	// Create a tiny module with two packages.
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "p"), 0755); err != nil {
		t.Fatalf("mkdir p: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nfunc P() {}\n"), 0644); err != nil {
		t.Fatalf("write p.go: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "q"), 0755); err != nil {
		t.Fatalf("mkdir q: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "q", "q.go"), []byte("package q\n\nfunc Q() {}\n"), 0644); err != nil {
		t.Fatalf("write q.go: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "context", "packages", "--search", "q$"}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
	}
	got := out.String()
	if strings.Contains(got, "example.com/tmpmod/p") {
		t.Fatalf("expected output to omit package p, got:\n%s", got)
	}
	if !strings.Contains(got, "example.com/tmpmod/q") {
		t.Fatalf("expected output to include package q, got:\n%s", got)
	}
}

func TestRun_ContextPackages_ExtraArg_IsUsageError(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "context", "packages", "nope"}, &RunOptions{Out: &out, Err: &errOut})
	if err == nil {
		t.Fatalf("expected non-nil error")
	}
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d (err=%v)", code, err)
	}
	if errOut.Len() == 0 {
		t.Fatalf("expected stderr output for usage error")
	}
}

func TestRun_DocsAdd_MissingArg_IsUsageError(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "docs", "add"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.NotEmpty(t, errOut.String())
}

func TestRun_DocsAdd_UsesPackageLoadingFlagsAndConfig(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(`{
  "providerkeys": { "openai": "sk-from-config" },
  "preferredprovider": "anthropic",
  "reflowwidth": 77
}`), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "p"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nfunc Foo() {}\n"), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	origRunDocubotAddDocs := runDocubotAddDocs
	t.Cleanup(func() { runDocubotAddDocs = origRunDocubotAddDocs })

	var gotPkg *gocode.Package
	var gotOpts docubot.AddDocsOptions
	runDocubotAddDocs = func(pkg *gocode.Package, opts docubot.AddDocsOptions) ([]*gopackagediff.Change, error) {
		gotPkg = pkg
		gotOpts = opts
		return nil, nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{
		"codalotl", "docs", "add",
		"--public-only",
		"--include-test",
		"example.com/tmpmod/p/",
	}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.NotNil(t, gotPkg)
	require.Equal(t, "example.com/tmpmod/p", gotPkg.ImportPath)
	gotPkgDir, err := filepath.EvalSymlinks(gotPkg.AbsolutePath())
	require.NoError(t, err)
	wantPkgDir, err := filepath.EvalSymlinks(filepath.Join(tmp, "p"))
	require.NoError(t, err)
	require.Equal(t, wantPkgDir, gotPkgDir)
	require.True(t, gotOpts.OnlyDocumentExportedIdentifiers)
	require.False(t, gotOpts.OnlyDocumentImportantIdentifiers)
	require.True(t, gotOpts.DocumentTestFiles)
	require.Equal(t, 77, gotOpts.ReflowMaxWidth)
	require.Equal(t, llmmodel.ProviderIDAnthropic.DefaultModel(), gotOpts.Model)
	require.Equal(t, "Applied 0 documentation change(s).\n", out.String())
}

func TestRun_DocsAdd_PassesImportantFlagAndIncludeTest(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "p"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nfunc Foo() {}\n"), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	origRunDocubotAddDocs := runDocubotAddDocs
	t.Cleanup(func() { runDocubotAddDocs = origRunDocubotAddDocs })

	var gotOpts docubot.AddDocsOptions
	runDocubotAddDocs = func(pkg *gocode.Package, opts docubot.AddDocsOptions) ([]*gopackagediff.Change, error) {
		gotOpts = opts
		return nil, nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "docs", "add", "--important", "--include-test", "./p"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.False(t, gotOpts.OnlyDocumentExportedIdentifiers)
	require.True(t, gotOpts.OnlyDocumentImportantIdentifiers)
	require.True(t, gotOpts.DocumentTestFiles)
	require.Equal(t, "Applied 0 documentation change(s).\n", out.String())
}

func TestRun_DocsAdd_ImportantAndPublicOnlyAreMutuallyExclusive(t *testing.T) {
	isolateUserConfig(t)

	origRunDocubotAddDocs := runDocubotAddDocs
	t.Cleanup(func() { runDocubotAddDocs = origRunDocubotAddDocs })

	called := false
	runDocubotAddDocs = func(pkg *gocode.Package, opts docubot.AddDocsOptions) ([]*gopackagediff.Change, error) {
		called = true
		return nil, nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "docs", "add", "--public-only", "--important", "."}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.Empty(t, out.String())
	require.Contains(t, errOut.String(), "--public-only and --important are mutually exclusive")
	require.False(t, called)
}

func TestDocsAddCommand_PassesCLIOutputWriterToDocubot(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "p"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nfunc Foo() {}\n"), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	origRunDocubotAddDocs := runDocubotAddDocs
	t.Cleanup(func() { runDocubotAddDocs = origRunDocubotAddDocs })

	var gotOpts docubot.AddDocsOptions
	runDocubotAddDocs = func(pkg *gocode.Package, opts docubot.AddDocsOptions) ([]*gopackagediff.Change, error) {
		gotOpts = opts
		return nil, nil
	}

	root, _ := newRootCommand(true)
	addCmd := requireCommand(t, root, "docs", "add")

	var out bytes.Buffer
	var errOut bytes.Buffer
	err = addCmd.Run(&qcli.Context{
		Context: context.Background(),
		Command: addCmd,
		Args:    []string{"./p"},
		Out:     &out,
		Err:     &errOut,
	})
	require.NoError(t, err)
	require.Same(t, &out, gotOpts.BaseOptions.Out)
	require.Empty(t, errOut.String())
	require.Equal(t, "Applied 0 documentation change(s).\n", out.String())
}

func TestDocsAddCommand_PassesCLIContextToDocubot(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "p"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nfunc Foo() {}\n"), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	origRunDocubotAddDocs := runDocubotAddDocs
	t.Cleanup(func() { runDocubotAddDocs = origRunDocubotAddDocs })

	type contextKey string

	ctxKey := contextKey("docs-add-test")
	ctxValue := "sentinel"
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey, ctxValue))
	cancel()

	runDocubotAddDocs = func(pkg *gocode.Package, opts docubot.AddDocsOptions) ([]*gopackagediff.Change, error) {
		require.Same(t, ctx, opts.BaseOptions.Context)
		require.Equal(t, ctxValue, opts.BaseOptions.Context.Value(ctxKey))
		require.ErrorIs(t, opts.BaseOptions.Context.Err(), context.Canceled)
		return nil, nil
	}

	root, _ := newRootCommand(true)
	addCmd := requireCommand(t, root, "docs", "add")

	var out bytes.Buffer
	var errOut bytes.Buffer
	err = addCmd.Run(&qcli.Context{
		Context: ctx,
		Command: addCmd,
		Args:    []string{"./p"},
		Out:     &out,
		Err:     &errOut,
	})
	require.NoError(t, err)
	require.Empty(t, errOut.String())
	require.Equal(t, "Applied 0 documentation change(s).\n", out.String())
}

func TestRun_DocsAdd_WritesDocubotProgressToCLIOutput(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "p"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nfunc Foo() {}\n"), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	origRunDocubotAddDocs := runDocubotAddDocs
	t.Cleanup(func() { runDocubotAddDocs = origRunDocubotAddDocs })

	runDocubotAddDocs = func(pkg *gocode.Package, opts docubot.AddDocsOptions) ([]*gopackagediff.Change, error) {
		_, err := opts.BaseOptions.Out.Write([]byte("Generating docs...\n"))
		require.NoError(t, err)
		return nil, nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "docs", "add", "./p"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, "Generating docs...\nApplied 0 documentation change(s).\n", out.String())
}

func TestRun_Config_PrintsJSON(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755); err != nil {
		t.Fatalf("mkdir .codalotl: %v", err)
	}
	cfgPath := filepath.Join(tmp, ".codalotl", "config.json")
	cfg := `{
  "providerkeys": { "openai": "sk-test" },
  "reflowwidth": 80,
  "theme": "dark"
}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", errOut.String())
	}

	if !strings.Contains(out.String(), "Current Configuration:") {
		t.Fatalf("expected output to include header, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Current Config Location(s):") {
		t.Fatalf("expected output to include config locations header, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), cfgPath) {
		t.Fatalf("expected output to include config file path %q, got:\n%s", cfgPath, out.String())
	}
	if !strings.Contains(out.String(), "Effective Model:") {
		t.Fatalf("expected output to include effective model, got:\n%s", out.String())
	}

	cfgJSON := extractConfigJSON(t, out.String())

	var got Config
	if err := json.Unmarshal([]byte(cfgJSON), &got); err != nil {
		t.Fatalf("expected JSON config in output, unmarshal error: %v\nstdout=%q", err, out.String())
	}
	if got.ReflowWidth != 80 {
		t.Fatalf("expected reflowwidth=80, got %d", got.ReflowWidth)
	}
	if got.Theme != "dark" {
		t.Fatalf("expected theme=dark, got %q", got.Theme)
	}
	if got.ProviderKeys.OpenAI != "*******" {
		t.Fatalf("expected providerkeys.openai to be redacted, got %q", got.ProviderKeys.OpenAI)
	}
}

func TestRun_Config_Defaults(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
	}

	if !strings.Contains(out.String(), "Current Config Location(s): (none") {
		t.Fatalf("expected output to mention no config locations, got:\n%s", out.String())
	}

	cfgJSON := extractConfigJSON(t, out.String())

	var got Config
	if err := json.Unmarshal([]byte(cfgJSON), &got); err != nil {
		t.Fatalf("expected JSON config in output, unmarshal error: %v\nstdout=%q", err, out.String())
	}
	if got.ReflowWidth != 120 {
		t.Fatalf("expected default reflowwidth=120, got %d", got.ReflowWidth)
	}

	wantEffective := string(llmmodel.ModelIDOrFallback(""))
	if !strings.Contains(out.String(), "Effective Model: "+wantEffective) {
		t.Fatalf("expected output to mention effective model %q, got:\n%s", wantEffective, out.String())
	}
}

func TestRun_Config_NoLLMConfigured_IsExitCode1(t *testing.T) {
	isolateUserConfig(t)

	// Explicitly remove the default key provided by isolateUserConfig.
	t.Setenv("OPENAI_API_KEY", "")

	// Ensure this test doesn't accidentally pick up a project config from the
	// repo (ex: .codalotl/config.json).
	tmp := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	if err == nil {
		t.Fatalf("expected non-nil error")
	}
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d (err=%v)", code, err)
	}
	if errOut.Len() == 0 {
		t.Fatalf("expected stderr output")
	}
	got := errOut.String()
	require.Contains(t, got, "No usable LLM auth or credentials are configured")
	require.NotContains(t, got, "No LLM provider API key is configured")
	require.Contains(t, got, "OPENAI_API_KEY")
	require.Contains(t, got, "ANTHROPIC_API_KEY")
	require.Contains(t, got, "GEMINI_API_KEY")
	require.Contains(t, got, "codalotl auth openai login")
}

func TestRun_Config_MissingLLMIncludesOpenAISubscriptionRefreshError(t *testing.T) {
	isolateUserConfig(t)
	restoreOpenAISubscriptionRefreshStub(t)

	t.Setenv("OPENAI_API_KEY", "")
	refreshOpenAIDefaultProviderSubscription = func(ctx context.Context) error {
		require.NotNil(t, ctx)
		return errors.New("refresh failed")
	}

	tmp := t.TempDir()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 1, code)
	require.Empty(t, out.String())

	got := errOut.String()
	require.Contains(t, got, "No usable LLM auth or credentials are configured")
	require.Contains(t, got, "OpenAI subscription auth could not be loaded/refreshed")
	require.Contains(t, got, "refresh failed")
}

func TestRun_StartupValidationRefreshesOpenAISubscriptionBeforeMissingAuth(t *testing.T) {
	isolateUserConfig(t)
	restoreOpenAISubscriptionRefreshStub(t)
	t.Setenv("OPENAI_API_KEY", "")

	tmp := t.TempDir()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	t.Cleanup(func() {
		llmmodel.ClearProviderSubscription(llmmodel.ProviderIDOpenAI)
	})

	var refreshCalls int
	refreshOpenAIDefaultProviderSubscription = func(ctx context.Context) error {
		refreshCalls++
		require.NotNil(t, ctx)
		require.Empty(t, llmmodel.AvailableModelIDsWithAuth())
		llmmodel.SetProviderSubscription(llmmodel.ProviderIDOpenAI, llmmodel.ProviderSubscription{
			ProviderID:      llmmodel.ProviderIDOpenAI,
			AccessToken:     "test-access-token",
			AccountID:       "test-account-id",
			APIEndpointURL:  "https://chatgpt.com/backend-api/codex",
			ExpiresAt:       time.Now().Add(time.Hour),
			RequiresNoStore: true,
		})
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Equal(t, 1, refreshCalls)
	require.Empty(t, errOut.String())
	require.Contains(t, out.String(), "Current Configuration:")
}

func TestRun_Config_DefaultOpenAIModelRefreshesOpenAISubscriptionWhenOtherProviderHasAuth(t *testing.T) {
	isolateUserConfig(t)
	restoreOpenAISubscriptionRefreshStub(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	require.Equal(t, llmmodel.ProviderIDOpenAI, effectiveModel(Config{}).ProviderID())

	tmp := t.TempDir()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	t.Cleanup(func() {
		llmmodel.ClearProviderSubscription(llmmodel.ProviderIDOpenAI)
	})

	var refreshCalls int
	refreshOpenAIDefaultProviderSubscription = func(ctx context.Context) error {
		refreshCalls++
		require.NotNil(t, ctx)
		require.NotEmpty(t, llmmodel.AvailableModelIDsWithAuth())
		llmmodel.SetProviderSubscription(llmmodel.ProviderIDOpenAI, llmmodel.ProviderSubscription{
			ProviderID:      llmmodel.ProviderIDOpenAI,
			AccessToken:     "test-access-token",
			AccountID:       "test-account-id",
			APIEndpointURL:  "https://chatgpt.com/backend-api/codex",
			ExpiresAt:       time.Now().Add(time.Hour),
			RequiresNoStore: true,
		})
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Equal(t, 1, refreshCalls)
	require.Empty(t, errOut.String())
	require.Contains(t, out.String(), "Current Configuration:")
	require.True(t, llmmodel.ProviderHasSubscription(llmmodel.ProviderIDOpenAI))
}

func TestRun_Config_APIKeyAuthRefreshesOpenAISubscriptionBeforeOpenAIFallback(t *testing.T) {
	isolateUserConfig(t)
	restoreOpenAISubscriptionRefreshStub(t)

	tmp := t.TempDir()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var refreshCalls int
	refreshOpenAIDefaultProviderSubscription = func(ctx context.Context) error {
		refreshCalls++
		require.NotNil(t, ctx)
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Equal(t, 1, refreshCalls)
	require.Empty(t, errOut.String())
	require.Contains(t, out.String(), "Current Configuration:")
}

func TestRun_Config_OpenAIAPIKeyDoesNotBypassUnusableSavedOpenAISubscription(t *testing.T) {
	isolateUserConfig(t)
	restoreOpenAISubscriptionRefreshStub(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	t.Cleanup(func() {
		llmmodel.SetProviderSubscriptionRequired(llmmodel.ProviderIDOpenAI, false)
	})

	tmp := t.TempDir()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var refreshCalls int
	refreshOpenAIDefaultProviderSubscription = func(ctx context.Context) error {
		refreshCalls++
		require.NotNil(t, ctx)
		llmmodel.SetProviderSubscriptionRequired(llmmodel.ProviderIDOpenAI, true)
		return errors.New("saved auth expired")
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 1, code)
	require.Equal(t, 1, refreshCalls)
	require.Empty(t, out.String())

	got := errOut.String()
	require.Contains(t, got, "OpenAI ChatGPT subscription auth is configured but unusable")
	require.Contains(t, got, "saved auth expired")
	require.Contains(t, got, "codalotl auth openai login")
	require.Contains(t, got, "codalotl auth openai logout")
	require.NotContains(t, got, "No usable LLM auth or credentials are configured")
}

func TestRun_Config_NonOpenAIEffectiveModelSkipsOpenAISubscriptionRefresh(t *testing.T) {
	isolateUserConfig(t)
	restoreOpenAISubscriptionRefreshStub(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")

	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(`{
  "preferredprovider": "anthropic"
}`), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var refreshCalls int
	refreshOpenAIDefaultProviderSubscription = func(ctx context.Context) error {
		refreshCalls++
		require.NotNil(t, ctx)
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Equal(t, 0, refreshCalls)
	require.Empty(t, errOut.String())
	require.Contains(t, out.String(), "Effective Model: "+string(llmmodel.ProviderIDAnthropic.DefaultModel()))
}

func TestRun_Config_NonOpenAIEffectiveModelRefreshesOpenAISubscriptionBeforeMissingAuth(t *testing.T) {
	isolateUserConfig(t)
	restoreOpenAISubscriptionRefreshStub(t)
	t.Setenv("OPENAI_API_KEY", "")

	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(`{
  "preferredprovider": "anthropic"
}`), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	t.Cleanup(func() {
		llmmodel.ClearProviderSubscription(llmmodel.ProviderIDOpenAI)
	})

	var refreshCalls int
	refreshOpenAIDefaultProviderSubscription = func(ctx context.Context) error {
		refreshCalls++
		require.NotNil(t, ctx)
		require.Empty(t, llmmodel.AvailableModelIDsWithAuth())
		llmmodel.SetProviderSubscription(llmmodel.ProviderIDOpenAI, llmmodel.ProviderSubscription{
			ProviderID:      llmmodel.ProviderIDOpenAI,
			AccessToken:     "test-access-token",
			AccountID:       "test-account-id",
			APIEndpointURL:  "https://chatgpt.com/backend-api/codex",
			ExpiresAt:       time.Now().Add(time.Hour),
			RequiresNoStore: true,
		})
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Equal(t, 1, refreshCalls)
	require.Empty(t, errOut.String())
	require.Contains(t, out.String(), "Effective Model: "+string(llmmodel.ProviderIDAnthropic.DefaultModel()))
}

func TestRun_Exec_ModelFlagRefreshesOpenAISubscriptionWhenConfigUsesOtherProvider(t *testing.T) {
	isolateUserConfig(t)
	restoreOpenAISubscriptionRefreshStub(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")

	openAIModel := llmmodel.ProviderIDOpenAI.DefaultModel()
	anthropicModel := llmmodel.ProviderIDAnthropic.DefaultModel()

	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(`{
  "preferredmodel": "`+string(anthropicModel)+`"
}`), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	t.Cleanup(func() {
		llmmodel.ClearProviderSubscription(llmmodel.ProviderIDOpenAI)
		llmmodel.SetProviderSubscriptionRequired(llmmodel.ProviderIDOpenAI, false)
	})

	llmmodel.SetProviderSubscriptionRequired(llmmodel.ProviderIDOpenAI, true)

	var refreshCalls int
	refreshOpenAIDefaultProviderSubscription = func(ctx context.Context) error {
		refreshCalls++
		require.NotNil(t, ctx)
		require.NotEmpty(t, llmmodel.AvailableModelIDsWithAuth())
		llmmodel.SetProviderSubscription(llmmodel.ProviderIDOpenAI, llmmodel.ProviderSubscription{
			ProviderID:      llmmodel.ProviderIDOpenAI,
			AccessToken:     "test-access-token",
			AccountID:       "test-account-id",
			APIEndpointURL:  "https://chatgpt.com/backend-api/codex",
			ExpiresAt:       time.Now().Add(time.Hour),
			RequiresNoStore: true,
		})
		return nil
	}

	origRunNoninteractiveExec := runNoninteractiveExec
	t.Cleanup(func() { runNoninteractiveExec = origRunNoninteractiveExec })

	var gotPrompt string
	var gotOpts noninteractive.Options
	runNoninteractiveExec = func(userPrompt string, opts noninteractive.Options) error {
		gotPrompt = userPrompt
		gotOpts = opts
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "exec", "--model", string(openAIModel), "hello"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Equal(t, 1, refreshCalls)
	require.Empty(t, errOut.String())
	require.Equal(t, "hello", gotPrompt)
	require.Equal(t, openAIModel, gotOpts.ModelID)
	require.True(t, llmmodel.ProviderHasSubscription(llmmodel.ProviderIDOpenAI))
}

func TestRun_Exec_ModelFlagFailsUnusableOpenAISubscriptionWhenConfigUsesOtherProvider(t *testing.T) {
	isolateUserConfig(t)
	restoreOpenAISubscriptionRefreshStub(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")

	openAIModel := llmmodel.ProviderIDOpenAI.DefaultModel()
	anthropicModel := llmmodel.ProviderIDAnthropic.DefaultModel()

	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(`{
  "preferredmodel": "`+string(anthropicModel)+`"
}`), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	t.Cleanup(func() {
		llmmodel.SetProviderSubscriptionRequired(llmmodel.ProviderIDOpenAI, false)
	})

	var refreshCalls int
	refreshOpenAIDefaultProviderSubscription = func(ctx context.Context) error {
		refreshCalls++
		require.NotNil(t, ctx)
		require.NotEmpty(t, llmmodel.AvailableModelIDsWithAuth())
		llmmodel.SetProviderSubscriptionRequired(llmmodel.ProviderIDOpenAI, true)
		return errors.New("saved auth expired")
	}

	origRunNoninteractiveExec := runNoninteractiveExec
	t.Cleanup(func() { runNoninteractiveExec = origRunNoninteractiveExec })

	var execCalled bool
	runNoninteractiveExec = func(string, noninteractive.Options) error {
		execCalled = true
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "exec", "--model", string(openAIModel), "hello"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 1, code)
	require.Equal(t, 1, refreshCalls)
	require.False(t, execCalled)
	require.Empty(t, out.String())

	got := errOut.String()
	require.Contains(t, got, "OpenAI ChatGPT subscription auth is configured but unusable")
	require.Contains(t, got, "saved auth expired")
	require.Contains(t, got, "codalotl auth openai login")
	require.Contains(t, got, "codalotl auth openai logout")
	require.NotContains(t, got, "No usable LLM auth or credentials are configured")
}

func TestRun_Config_SubscriptionAuthOnly_Succeeds(t *testing.T) {
	isolateUserConfig(t)
	restoreOpenAISubscriptionRefreshStub(t)

	// Prove startup validation accepts provider subscription auth without any
	// API-key-based model availability.
	t.Setenv("OPENAI_API_KEY", "")
	var refreshCalls int
	refreshOpenAIDefaultProviderSubscription = func(ctx context.Context) error {
		refreshCalls++
		require.NotNil(t, ctx)
		return nil
	}
	llmmodel.SetProviderSubscription(llmmodel.ProviderIDOpenAI, llmmodel.ProviderSubscription{
		ProviderID:      llmmodel.ProviderIDOpenAI,
		AccessToken:     "test-access-token",
		AccountID:       "test-account-id",
		APIEndpointURL:  "https://chatgpt.com/backend-api/codex",
		ExpiresAt:       time.Now().Add(time.Hour),
		RequiresNoStore: true,
	})
	t.Cleanup(func() {
		llmmodel.ClearProviderSubscription(llmmodel.ProviderIDOpenAI)
	})

	tmp := t.TempDir()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Equal(t, 0, refreshCalls)
	require.Empty(t, errOut.String())
	require.Contains(t, out.String(), "Current Configuration:")
}

func TestRun_Config_MissingTools_IsExitCode1(t *testing.T) {
	isolateUserConfig(t)

	// Deliberately make tools undiscoverable.
	t.Setenv("PATH", "")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	if err == nil {
		t.Fatalf("expected non-nil error")
	}
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d (err=%v)", code, err)
	}
	if errOut.Len() == 0 {
		t.Fatalf("expected stderr output")
	}
	if !strings.Contains(errOut.String(), "Missing required tools") {
		t.Fatalf("expected error to mention missing tools, got stderr:\n%s", errOut.String())
	}
	// Ensure we include go-install hints for the tools we can.
	if !strings.Contains(errOut.String(), "go install golang.org/x/tools/gopls@latest") {
		t.Fatalf("expected error to include gopls install hint, got stderr:\n%s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "go install golang.org/x/tools/cmd/goimports@latest") {
		t.Fatalf("expected error to include goimports install hint, got stderr:\n%s", errOut.String())
	}
}

func TestRun_Config_EnvVarsList_OnlyExposedProviders(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
	}

	got := extractEnvVarsList(t, out.String())

	want := map[string]bool{}
	envVars := llmmodel.ProviderKeyEnvVars()
	tpk := reflect.TypeOf(ProviderKeys{})
	for i := 0; i < tpk.NumField(); i++ {
		pid := providerIDFromProviderKeysField(tpk.Field(i))
		if pid == llmmodel.ProviderIDUnknown || !isKnownProviderID(pid) {
			continue
		}
		ev := strings.TrimSpace(envVars[pid])
		if ev == "" {
			continue
		}
		want[ev] = true
	}

	if len(got) != len(want) {
		t.Fatalf("unexpected env var list.\nwant=%v\ngot=%v\nstdout=%q", keys(want), keys(got), out.String())
	}
	for ev := range want {
		if !got[ev] {
			t.Fatalf("expected env var list to include %q.\nwant=%v\ngot=%v\nstdout=%q", ev, keys(want), keys(got), out.String())
		}
	}
	for ev := range got {
		if !want[ev] {
			t.Fatalf("expected env var list to omit %q.\nwant=%v\ngot=%v\nstdout=%q", ev, keys(want), keys(got), out.String())
		}
	}
}

func TestRun_Config_IgnoresUnexposedProviderKeys(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755); err != nil {
		t.Fatalf("mkdir .codalotl: %v", err)
	}
	cfg := `{
  "providerkeys": { "xai": "nope" }
}`
	if err := os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(cfg), 0644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	// Unexposed provider key fields should not cause the CLI to fail (they are
	// simply ignored by the current config schema).
	{
		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
		if err != nil {
			t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
		}

		cfgJSON := extractConfigJSON(t, out.String())
		if strings.Contains(strings.ToLower(cfgJSON), "xai") {
			t.Fatalf("expected config JSON to omit unknown providerkeys fields, got:\n%s", cfgJSON)
		}
	}

	// `version` should ignore config entirely.
	{
		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{"codalotl", "version"}, &RunOptions{Out: &out, Err: &errOut})
		if err != nil {
			t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
		}
	}

	// `-h` should ignore config entirely.
	{
		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{"codalotl", "-h"}, &RunOptions{Out: &out, Err: &errOut})
		if err != nil {
			t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
		}
		if out.Len() == 0 {
			t.Fatalf("expected help output on stdout")
		}
	}
}

func TestRun_ContextPackages_UnexposedProviderKey_StillSucceeds(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()

	// Create a tiny module with one package (so the command would succeed if
	// config were valid).
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "p"), 0755); err != nil {
		t.Fatalf("mkdir p: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nfunc P() {}\n"), 0644); err != nil {
		t.Fatalf("write p.go: %v", err)
	}

	// Write a config with an unexposed provider key field; this should not break
	// the CLI.
	if err := os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755); err != nil {
		t.Fatalf("mkdir .codalotl: %v", err)
	}
	cfg := `{
  "providerkeys": { "xai": "nope" }
}`
	if err := os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(cfg), 0644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "context", "packages"}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "example.com/tmpmod/p") {
		t.Fatalf("expected output to include package p, got:\n%s", out.String())
	}
}

func TestRun_Config_UsesEnvProviderKeyWhenConfigEmpty(t *testing.T) {
	isolateUserConfig(t)

	t.Setenv("OPENAI_API_KEY", "sk-envtest")

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755); err != nil {
		t.Fatalf("mkdir .codalotl: %v", err)
	}
	cfg := `{
  "providerkeys": { "openai": "" }
}`
	if err := os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(cfg), 0644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
	}

	cfgJSON := extractConfigJSON(t, out.String())
	var got Config
	if err := json.Unmarshal([]byte(cfgJSON), &got); err != nil {
		t.Fatalf("expected JSON config in output, unmarshal error: %v\nstdout=%q", err, out.String())
	}
	if got.ProviderKeys.OpenAI != "sk-e...test" {
		t.Fatalf("expected providerkeys.openai to fall back to env (redacted), got %q", got.ProviderKeys.OpenAI)
	}
}

func TestRun_Config_UsesEnvProviderKeyWhenConfigIsStarsPlaceholder(t *testing.T) {
	isolateUserConfig(t)

	t.Setenv("OPENAI_API_KEY", "sk-envtest")

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755); err != nil {
		t.Fatalf("mkdir .codalotl: %v", err)
	}
	cfg := `{
  "providerkeys": { "openai": "***" }
}`
	if err := os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(cfg), 0644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
	}

	cfgJSON := extractConfigJSON(t, out.String())
	var got Config
	if err := json.Unmarshal([]byte(cfgJSON), &got); err != nil {
		t.Fatalf("expected JSON config in output, unmarshal error: %v\nstdout=%q", err, out.String())
	}
	if got.ProviderKeys.OpenAI != "sk-e...test" {
		t.Fatalf("expected providerkeys.openai to treat '***' as placeholder and fall back to env (redacted), got %q", got.ProviderKeys.OpenAI)
	}
}

func TestRun_Config_ConfiguresProviderKeyForLlmmodel(t *testing.T) {
	isolateUserConfig(t)

	// Prove config-file key overrides env default by ensuring env is set to a
	// different value.
	t.Setenv("OPENAI_API_KEY", "sk-env-default")

	// llmmodel key overrides are process-global; clear and restore for test
	// isolation.
	llmmodel.ConfigureProviderKey(llmmodel.ProviderIDOpenAI, "")
	t.Cleanup(func() {
		llmmodel.ConfigureProviderKey(llmmodel.ProviderIDOpenAI, "")
	})

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755); err != nil {
		t.Fatalf("mkdir .codalotl: %v", err)
	}
	cfg := `{
  "providerkeys": { "openai": "sk-from-config" }
}`
	if err := os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(cfg), 0644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
	}

	if got := llmmodel.GetAPIKey(llmmodel.DefaultModel); got != "sk-from-config" {
		t.Fatalf("expected llmmodel to use config-file key, got %q", got)
	}
}

func TestRun_Config_DoesNotConfigurePlaceholderProviderKeyForLlmmodel(t *testing.T) {
	isolateUserConfig(t)

	t.Setenv("OPENAI_API_KEY", "sk-env-default")

	// llmmodel key overrides are process-global; clear and restore for test
	// isolation.
	llmmodel.ConfigureProviderKey(llmmodel.ProviderIDOpenAI, "")
	t.Cleanup(func() {
		llmmodel.ConfigureProviderKey(llmmodel.ProviderIDOpenAI, "")
	})

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755); err != nil {
		t.Fatalf("mkdir .codalotl: %v", err)
	}
	cfg := `{
  "providerkeys": { "openai": "***" }
}`
	if err := os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(cfg), 0644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("expected nil error, got %v (stderr=%q)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, errOut.String())
	}

	if got := llmmodel.GetAPIKey(llmmodel.DefaultModel); got != "sk-env-default" {
		t.Fatalf("expected llmmodel to fall back to env default key, got %q", got)
	}
}

func extractConfigJSON(t *testing.T, stdout string) string {
	t.Helper()

	start := strings.Index(stdout, "{")
	if start < 0 {
		t.Fatalf("expected output to contain JSON object, got:\n%s", stdout)
	}

	dec := json.NewDecoder(strings.NewReader(stdout[start:]))
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		t.Fatalf("expected JSON object in output, decode error: %v\nstdout=%q", err, stdout)
	}
	cfgJSON := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(cfgJSON, "{") || !strings.HasSuffix(cfgJSON, "}") {
		t.Fatalf("expected JSON block to be a single object, got:\n%s", cfgJSON)
	}
	return cfgJSON
}

func extractEnvVarsList(t *testing.T, stdout string) map[string]bool {
	t.Helper()

	const header = "To set LLM provider API keys, set one of these ENV variables:"
	start := strings.Index(stdout, header)
	if start < 0 {
		t.Fatalf("expected output to contain env var list header %q, got:\n%s", header, stdout)
	}

	rest := stdout[start+len(header):]
	rest = strings.TrimPrefix(rest, "\n")

	got := map[string]bool{}
	for _, line := range strings.Split(rest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		ev := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if ev != "" {
			got[ev] = true
		}
	}
	return got
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func requireCommand(t *testing.T, root *qcli.Command, names ...string) *qcli.Command {
	t.Helper()

	cmd := root
	for _, name := range names {
		var next *qcli.Command
		for _, child := range cmd.Commands() {
			if child.Name == name {
				next = child
				break
			}
		}
		require.NotNil(t, next)
		cmd = next
	}

	return cmd
}
