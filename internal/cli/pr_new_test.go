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
