package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRun_PRNew_NoGit_CreatesPRFileWithoutConfigOrGit(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("PATH", "")

	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(`{not-json`), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	origNow := prNewNow
	prNewNow = func() time.Time { return time.Unix(1779211919, 0).UTC() }
	t.Cleanup(func() { prNewNow = origNow })

	origGit := runPRNewGit
	runPRNewGit = func(context.Context, string, ...string) (string, error) {
		return "", errors.New("git should not be called")
	}
	t.Cleanup(func() { runPRNewGit = origGit })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "pr", "new", "cas-prune", "--no-git"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, "Created .prs/2026-05-19_1779211919_cas-prune.md\n", out.String())

	prPath := filepath.Join(tmp, ".prs", "2026-05-19_1779211919_cas-prune.md")
	got, err := os.ReadFile(prPath)
	require.NoError(t, err)
	require.Equal(t, prNewInitialTemplate, string(got))

	code, err = Run([]string{"codalotl", "pr", "new", "cas-prune", "--no-git"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 1, code)

	got, err = os.ReadFile(prPath)
	require.NoError(t, err)
	require.Equal(t, prNewInitialTemplate, string(got))
}

func TestRun_PRNew_NormalMode_ValidatesGitCreatesBranchCommitsAndPushes(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("PATH", "")
	t.Setenv("CODALOTL_USER_INITIALS", "jn")

	repo := t.TempDir()
	cwd := filepath.Join(repo, "subdir")
	require.NoError(t, os.MkdirAll(cwd, 0755))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(cwd))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	origNow := prNewNow
	prNewNow = func() time.Time { return time.Unix(1779229784, 0).UTC() }
	t.Cleanup(func() { prNewNow = origNow })

	var calls []prNewGitCall
	stubPRNewGit(t, func(dir string, args []string) (string, error) {
		calls = append(calls, prNewGitCall{dir: dir, args: append([]string(nil), args...)})
		switch strings.Join(args, "\x00") {
		case "rev-parse\x00--show-toplevel":
			return repo + "\n", nil
		case "status\x00--porcelain":
			return "", nil
		case "branch\x00--show-current":
			return "main\n", nil
		case "for-each-ref\x00--format=%(upstream:short)\x00refs/heads/main":
			return "origin/main\n", nil
		case "rev-list\x00--left-right\x00--count\x00HEAD...@{u}":
			return "0\t0\n", nil
		case "checkout\x00-b\x00jn/add-orchestrator-pr-new":
			return "", nil
		case "add\x00.prs/2026-05-19_1779229784_add-orchestrator-pr-new.md":
			return "", nil
		case "commit\x00-m\x00Add PR file for add-orchestrator-pr-new":
			return "", nil
		case "remote\x00get-url\x00origin":
			return "git@example.com:repo.git\n", nil
		case "push\x00-u\x00origin\x00jn/add-orchestrator-pr-new":
			return "", nil
		default:
			t.Fatalf("unexpected git command in %s: %q", dir, args)
			return "", nil
		}
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "pr", "new", "add-orchestrator-pr-new"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, "Created .prs/2026-05-19_1779229784_add-orchestrator-pr-new.md\n", out.String())

	prPath := filepath.Join(repo, ".prs", "2026-05-19_1779229784_add-orchestrator-pr-new.md")
	got, err := os.ReadFile(prPath)
	require.NoError(t, err)
	require.Equal(t, prNewInitialTemplate, string(got))

	require.Equal(t, [][]string{
		{"rev-parse", "--show-toplevel"},
		{"status", "--porcelain"},
		{"branch", "--show-current"},
		{"for-each-ref", "--format=%(upstream:short)", "refs/heads/main"},
		{"rev-list", "--left-right", "--count", "HEAD...@{u}"},
		{"checkout", "-b", "jn/add-orchestrator-pr-new"},
		{"add", ".prs/2026-05-19_1779229784_add-orchestrator-pr-new.md"},
		{"commit", "-m", "Add PR file for add-orchestrator-pr-new"},
		{"remote", "get-url", "origin"},
		{"push", "-u", "origin", "jn/add-orchestrator-pr-new"},
	}, prNewGitCallArgs(calls))
	gotCWD, err := filepath.EvalSymlinks(calls[0].dir)
	require.NoError(t, err)
	wantCWD, err := filepath.EvalSymlinks(cwd)
	require.NoError(t, err)
	require.Equal(t, wantCWD, gotCWD)
	for _, call := range calls[1:] {
		require.Equal(t, repo, call.dir)
	}
}

func TestRun_PRNew_NormalMode_AllowsLocalOnlyRepoWithoutUpstreamAndOrigin(t *testing.T) {
	isolateUserConfig(t)

	repo := t.TempDir()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	origNow := prNewNow
	prNewNow = func() time.Time { return time.Unix(1779229784, 0).UTC() }
	t.Cleanup(func() { prNewNow = origNow })

	var calls []prNewGitCall
	stubPRNewGit(t, func(dir string, args []string) (string, error) {
		calls = append(calls, prNewGitCall{dir: dir, args: append([]string(nil), args...)})
		switch args[0] {
		case "rev-parse":
			return repo + "\n", nil
		case "status":
			return "", nil
		case "branch":
			return "master\n", nil
		case "for-each-ref":
			return "\n", nil
		case "checkout", "add", "commit":
			return "", nil
		case "remote":
			return "", errors.New("no such remote")
		default:
			t.Fatalf("unexpected git command: %q", args)
			return "", nil
		}
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "pr", "new", "without-origin"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, "Created .prs/2026-05-19_1779229784_without-origin.md\n", out.String())

	prPath := filepath.Join(repo, ".prs", "2026-05-19_1779229784_without-origin.md")
	got, err := os.ReadFile(prPath)
	require.NoError(t, err)
	require.Equal(t, prNewInitialTemplate, string(got))

	flattenedCalls := flattenPRNewGitCalls(calls)
	require.NotContains(t, flattenedCalls, "HEAD...@{u}")
	require.NotContains(t, flattenedCalls, "push")
}

func TestRun_PRNew_NormalMode_DirtyWorkspaceFailsBeforeCreatingFile(t *testing.T) {
	isolateUserConfig(t)

	repo := t.TempDir()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	origNow := prNewNow
	prNewNow = func() time.Time { return time.Unix(1779229784, 0).UTC() }
	t.Cleanup(func() { prNewNow = origNow })

	stubPRNewGit(t, func(_ string, args []string) (string, error) {
		switch args[0] {
		case "rev-parse":
			return repo + "\n", nil
		case "status":
			return " M file.go\n", nil
		default:
			t.Fatalf("unexpected git command after dirty status: %q", args)
			return "", nil
		}
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "pr", "new", "dirty-workspace"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 1, code)
	require.Empty(t, out.String())
	require.Contains(t, errOut.String(), "working tree is not clean")
	_, statErr := os.Stat(filepath.Join(repo, ".prs", "2026-05-19_1779229784_dirty-workspace.md"))
	require.True(t, os.IsNotExist(statErr))
}

func TestRun_PRNew_NormalMode_GitUnavailableIsCommandSpecificError(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("PATH", "")

	tmp := t.TempDir()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "pr", "new", "needs-git"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 1, code)
	require.Empty(t, out.String())
	require.Contains(t, errOut.String(), "git rev-parse --show-toplevel")
	require.NotContains(t, errOut.String(), "No LLM provider API key is configured")
	require.NotContains(t, errOut.String(), "Missing required tools")
}

func TestRun_PRNew_InvalidFeatureNameIsUsageError(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "pr", "new", "../bad", "--no-git"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.Empty(t, out.String())
	require.Contains(t, errOut.String(), "invalid <feature-name>")
}

func TestRun_PRRefactor_NormalMode_ReusesPRNewGitBehaviorAndWritesInstructions(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("CODALOTL_USER_INITIALS", "")

	repo := t.TempDir()
	writeTestGoPackage(t, repo, "internal/mypkg")

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	origNow := prNewNow
	prNewNow = func() time.Time { return time.Unix(1779277562, 0).UTC() }
	t.Cleanup(func() { prNewNow = origNow })

	var calls []prNewGitCall
	stubPRNewGit(t, func(dir string, args []string) (string, error) {
		calls = append(calls, prNewGitCall{dir: dir, args: append([]string(nil), args...)})
		switch strings.Join(args, "\x00") {
		case "rev-parse\x00--show-toplevel":
			return repo + "\n", nil
		case "status\x00--porcelain":
			return "", nil
		case "branch\x00--show-current":
			return "main\n", nil
		case "for-each-ref\x00--format=%(upstream:short)\x00refs/heads/main":
			return "origin/main\n", nil
		case "rev-list\x00--left-right\x00--count\x00HEAD...@{u}":
			return "0\t0\n", nil
		case "checkout\x00-b\x00refactor-internal-mypkg":
			return "", nil
		case "add\x00.prs/2026-05-20_1779277562_refactor-internal-mypkg.md":
			return "", nil
		case "commit\x00-m\x00Add PR file for refactor-internal-mypkg":
			return "", nil
		case "remote\x00get-url\x00origin":
			return "git@example.com:repo.git\n", nil
		case "push\x00-u\x00origin\x00refactor-internal-mypkg":
			return "", nil
		default:
			t.Fatalf("unexpected git command in %s: %q", dir, args)
			return "", nil
		}
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "pr", "refactor", "--package=internal/mypkg"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, "Created .prs/2026-05-20_1779277562_refactor-internal-mypkg.md\n", out.String())

	require.Equal(t, [][]string{
		{"rev-parse", "--show-toplevel"},
		{"status", "--porcelain"},
		{"branch", "--show-current"},
		{"for-each-ref", "--format=%(upstream:short)", "refs/heads/main"},
		{"rev-list", "--left-right", "--count", "HEAD...@{u}"},
		{"checkout", "-b", "refactor-internal-mypkg"},
		{"add", ".prs/2026-05-20_1779277562_refactor-internal-mypkg.md"},
		{"commit", "-m", "Add PR file for refactor-internal-mypkg"},
		{"remote", "get-url", "origin"},
		{"push", "-u", "origin", "refactor-internal-mypkg"},
	}, prNewGitCallArgs(calls))

	prPath := filepath.Join(repo, ".prs", "2026-05-20_1779277562_refactor-internal-mypkg.md")
	gotBytes, err := os.ReadFile(prPath)
	require.NoError(t, err)
	got := string(gotBytes)
	require.True(t, strings.HasPrefix(got, "# PR\n\n## User Summary (do not modify)\n\n"))
	require.Contains(t, got, "Target package: internal/mypkg")
	require.Contains(t, got, "Selected refactor flow: all refactors for one package")
	require.Contains(t, got, `refactor("name": "docs-add", "package": "internal/mypkg")`)
	require.Contains(t, got, `refactor("name": "docs-fix", "package": "internal/mypkg")`)
	require.Contains(t, got, `refactor("name": "dry", "package": "internal/mypkg")`)
	require.Contains(t, got, `refactor("name": "test-cleanup", "package": "internal/mypkg")`)
	require.Contains(t, got, `refactor("name": "test-ensure-coverage", "package": "internal/mypkg")`)
	requireRefactorOrder(t, got, []string{"docs-add", "docs-fix", "dry", "test-cleanup", "test-ensure-coverage"})
	require.Contains(t, got, "inspect the diff")
	require.Contains(t, got, "commit that refactor separately")
	require.Contains(t, got, "relevant CAS files")
	require.Contains(t, got, "refactor result is a no-op")
	require.Contains(t, got, "avoid risky fix-forward behavior")
	require.Contains(t, got, "codalotl_cli")
	require.Contains(t, got, `codalotl cas recertify internal/mypkg --namespaces="docs-fix,refactor-dry,refactor-test-cleanup,refactor-test-ensure-coverage"`)
	require.NotContains(t, got, `--namespaces="docs-add`)
}

func TestRun_PRRefactor_PackageSingleRefactor_WritesFocusedInstructions(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("CODALOTL_USER_INITIALS", "")

	repo := t.TempDir()
	writeTestGoPackage(t, repo, "internal/mypkg")

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	origNow := prNewNow
	prNewNow = func() time.Time { return time.Unix(1779277562, 0).UTC() }
	t.Cleanup(func() { prNewNow = origNow })

	stubPRNewGitForScaffold(t, repo, "refactor-docs-fix-internal-mypkg", "2026-05-20_1779277562_refactor-docs-fix-internal-mypkg.md")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "pr", "refactor", "--package=internal/mypkg", "--refactor=docs-fix"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, "Created .prs/2026-05-20_1779277562_refactor-docs-fix-internal-mypkg.md\n", out.String())

	prPath := filepath.Join(repo, ".prs", "2026-05-20_1779277562_refactor-docs-fix-internal-mypkg.md")
	gotBytes, err := os.ReadFile(prPath)
	require.NoError(t, err)
	got := string(gotBytes)
	require.Contains(t, got, "Target package: internal/mypkg")
	require.Contains(t, got, "Selected refactor flow: docs-fix")
	require.Contains(t, got, `refactor("name": "docs-fix", "package": "internal/mypkg")`)
	require.NotContains(t, got, `refactor("name": "docs-add"`)
	require.NotContains(t, got, `refactor("name": "dry"`)
	require.Contains(t, got, "commit the accepted changes")
	require.Contains(t, got, "relevant CAS files")
	require.Contains(t, got, "skip it with a note in this PR file")
	require.Contains(t, got, "codalotl_cli")
	require.Contains(t, got, `codalotl cas recertify internal/mypkg --namespaces="docs-fix"`)
}

func TestRun_PRRefactor_AllPackagesSingleRefactor_WritesAllPackagesInstructions(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("CODALOTL_USER_INITIALS", "")

	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	origNow := prNewNow
	prNewNow = func() time.Time { return time.Unix(1779277562, 0).UTC() }
	t.Cleanup(func() { prNewNow = origNow })

	stubPRNewGitForScaffold(t, repo, "refactor-docs-fix-all-packages", "2026-05-20_1779277562_refactor-docs-fix-all-packages.md")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "pr", "refactor", "--all-packages", "--refactor=docs-fix"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, "Created .prs/2026-05-20_1779277562_refactor-docs-fix-all-packages.md\n", out.String())

	prPath := filepath.Join(repo, ".prs", "2026-05-20_1779277562_refactor-docs-fix-all-packages.md")
	gotBytes, err := os.ReadFile(prPath)
	require.NoError(t, err)
	got := string(gotBytes)
	require.Contains(t, got, "Target: all Go packages in the current module")
	require.Contains(t, got, "Selected refactor flow: docs-fix")
	require.Contains(t, got, `refactor("name": "docs-fix", "package": "<package>")`)
	require.Contains(t, got, "Inspect each refactor result")
	require.Contains(t, got, "Commit accepted changes")
	require.Contains(t, got, "relevant CAS files")
	require.Contains(t, got, "Skip no-op packages without a commit and add a note in this PR file")
	require.Contains(t, got, "add a note in this PR file")
	require.Contains(t, got, "codalotl_cli")
	require.Contains(t, got, `codalotl cas recertify <package> --namespaces="docs-fix"`)
}

func TestPRRefactorFeatureName(t *testing.T) {
	tests := []struct {
		packagePath string
		want        string
	}{
		{packagePath: "internal/cli", want: "refactor-internal-cli"},
		{packagePath: "./internal/cli", want: "refactor-internal-cli"},
		{packagePath: "github.com/codalotl/codalotl/internal/cli", want: "refactor-github.com-codalotl-codalotl-internal-cli"},
		{packagePath: "pkg with spaces", want: "refactor-pkg-with-spaces"},
		{packagePath: ".", want: "refactor-package"},
	}

	for _, tt := range tests {
		t.Run(tt.packagePath, func(t *testing.T) {
			got, err := prRefactorFeatureName(tt.packagePath)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestPRRefactorSingleFeatureNames(t *testing.T) {
	got, err := prRefactorPackageFeatureName("internal/cli", "docs-fix")
	require.NoError(t, err)
	require.Equal(t, "refactor-docs-fix-internal-cli", got)

	got, err = prRefactorAllPackagesFeatureName("docs-fix")
	require.NoError(t, err)
	require.Equal(t, "refactor-docs-fix-all-packages", got)
}

func TestRun_PRRefactor_ValidatesSelectorsAndRefactorName(t *testing.T) {
	isolateUserConfig(t)

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "missing selector",
			args:    []string{"codalotl", "pr", "refactor"},
			wantErr: "supply exactly one of --package or --all-packages",
		},
		{
			name:    "both selectors",
			args:    []string{"codalotl", "pr", "refactor", "--package=internal/mypkg", "--all-packages", "--refactor=docs-fix"},
			wantErr: "supply exactly one of --package or --all-packages",
		},
		{
			name:    "all packages requires refactor",
			args:    []string{"codalotl", "pr", "refactor", "--all-packages"},
			wantErr: "--all-packages requires --refactor",
		},
		{
			name:    "unsupported refactor",
			args:    []string{"codalotl", "pr", "refactor", "--package=internal/mypkg", "--refactor=unknown"},
			wantErr: `unsupported --refactor "unknown"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			var errOut bytes.Buffer
			code, err := Run(tt.args, &RunOptions{Out: &out, Err: &errOut})
			require.Error(t, err)
			require.Equal(t, 2, code)
			require.Empty(t, out.String())
			require.Contains(t, errOut.String(), tt.wantErr)
		})
	}
}

func TestRun_PRRefactor_BypassesStartupValidation(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("PATH", "")

	repo := t.TempDir()
	writeTestGoPackage(t, repo, "p")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".codalotl"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".codalotl", "config.json"), []byte(`{not-json`), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	origNow := prNewNow
	prNewNow = func() time.Time { return time.Unix(1779277562, 0).UTC() }
	t.Cleanup(func() { prNewNow = origNow })

	stubPRNewGit(t, func(_ string, args []string) (string, error) {
		switch args[0] {
		case "rev-parse":
			return repo + "\n", nil
		case "status":
			return "", nil
		case "branch":
			return "main\n", nil
		case "for-each-ref":
			return "\n", nil
		case "checkout", "add", "commit":
			return "", nil
		case "remote":
			return "", errors.New("no origin")
		default:
			t.Fatalf("unexpected git command: %q", args)
			return "", nil
		}
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "pr", "refactor", "--package=./p"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Contains(t, out.String(), "refactor-p.md")
	require.Empty(t, errOut.String())
	require.NotContains(t, errOut.String(), "No LLM provider API key is configured")
	require.NotContains(t, errOut.String(), "Missing required tools")
}

type prNewGitCall struct {
	dir  string
	args []string
}

func stubPRNewGit(t *testing.T, f func(dir string, args []string) (string, error)) {
	t.Helper()

	orig := runPRNewGit
	runPRNewGit = func(_ context.Context, dir string, args ...string) (string, error) {
		return f(dir, append([]string(nil), args...))
	}
	t.Cleanup(func() { runPRNewGit = orig })
}

func stubPRNewGitForScaffold(t *testing.T, repo string, branchName string, filename string) {
	t.Helper()

	stubPRNewGit(t, func(_ string, args []string) (string, error) {
		switch strings.Join(args, "\x00") {
		case "rev-parse\x00--show-toplevel":
			return repo + "\n", nil
		case "status\x00--porcelain":
			return "", nil
		case "branch\x00--show-current":
			return "main\n", nil
		case "for-each-ref\x00--format=%(upstream:short)\x00refs/heads/main":
			return "\n", nil
		case "checkout\x00-b\x00" + branchName:
			return "", nil
		case "add\x00.prs/" + filename:
			return "", nil
		case "commit\x00-m\x00Add PR file for " + branchName:
			return "", nil
		case "remote\x00get-url\x00origin":
			return "", errors.New("no origin")
		default:
			t.Fatalf("unexpected git command: %q", args)
			return "", nil
		}
	})
}

func prNewGitCallArgs(calls []prNewGitCall) [][]string {
	out := make([][]string, 0, len(calls))
	for _, call := range calls {
		out = append(out, call.args)
	}
	return out
}

func flattenPRNewGitCalls(calls []prNewGitCall) string {
	var lines []string
	for _, call := range calls {
		lines = append(lines, strings.Join(call.args, " "))
	}
	return strings.Join(lines, "\n")
}

func writeTestGoPackage(t *testing.T, moduleDir string, packageDir string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	dir := filepath.Join(moduleDir, filepath.FromSlash(packageDir))
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pkg.go"), []byte("package mypkg\n\nfunc Foo() {}\n"), 0644))
}

func requireRefactorOrder(t *testing.T, got string, names []string) {
	t.Helper()

	prev := -1
	for _, name := range names {
		idx := strings.Index(got, `refactor("name": "`+name+`"`)
		require.NotEqual(t, -1, idx)
		require.Greater(t, idx, prev)
		prev = idx
	}
}
