package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"
)

func isolateUserConfig(t *testing.T) {
	t.Helper()
	// Prevent tests from reading developer machine config (ex: ~/.codalotl/config.json).
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())

	// llmmodel key overrides are process-global; ensure tests don't leak state.
	for _, pid := range llmmodel.AllProviderIDs {
		llmmodel.ConfigureProviderKey(pid, "")
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
}

func ensureToolStubs(t *testing.T, names ...string) {
	t.Helper()

	binDir := t.TempDir()

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

func TestRun_Config_PrintsJSON(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755); err != nil {
		t.Fatalf("mkdir .codalotl: %v", err)
	}
	cfgPath := filepath.Join(tmp, ".codalotl", "config.json")
	cfg := `{
  "providerkeys": { "openai": "sk-test" },
  "maxwidth": 80
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
	if got.MaxWidth != 80 {
		t.Fatalf("expected maxwidth=80, got %d", got.MaxWidth)
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
	if got.MaxWidth != 120 {
		t.Fatalf("expected default maxwidth=120, got %d", got.MaxWidth)
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
	if !strings.Contains(errOut.String(), "No LLM provider API key is configured") {
		t.Fatalf("expected error to mention missing LLM key, got stderr:\n%s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "OPENAI_API_KEY") {
		t.Fatalf("expected error to mention OPENAI_API_KEY, got stderr:\n%s", errOut.String())
	}
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

func TestRun_Config_IgnoresUnknownProviderKeys(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755); err != nil {
		t.Fatalf("mkdir .codalotl: %v", err)
	}
	cfg := `{
  "providerkeys": { "anthropic": "nope" }
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

	// Unknown provider key fields should not cause the CLI to fail (they are
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
		if strings.Contains(strings.ToLower(cfgJSON), "anthropic") {
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

func TestRun_ContextPackages_InvalidProvider_IsExitCode1(t *testing.T) {
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

	// Write a config with an unknown provider key field; this should not break
	// the CLI.
	if err := os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755); err != nil {
		t.Fatalf("mkdir .codalotl: %v", err)
	}
	cfg := `{
  "providerkeys": { "anthropic": "nope" }
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
