package spectools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/gocas/casconformance"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetermineComparisonBaseMainBranchUsesHEAD(t *testing.T) {
	t.Parallel()

	tool := &toolCheckSpecConformance{
		git: fakeGitRunner{
			outputs: map[string]string{
				gitCommandKey("branch", "--show-current"): "main\n",
				gitCommandKey("rev-parse", "HEAD"):        "0123456789abcdef\n",
			},
		},
	}

	base, err := tool.determineComparisonBase(context.Background(), "/tmp/repo")
	require.NoError(t, err)
	assert.Equal(t, comparisonBase{
		Branch: "main",
		Commit: "0123456789abcdef",
		Mode:   comparisonBaseModeHEAD,
	}, base)
}

func TestDetermineComparisonBaseUsesCreationMessageWhenCandidatesAreAmbiguous(t *testing.T) {
	t.Parallel()

	tool := &toolCheckSpecConformance{
		git: fakeGitRunner{
			outputs: map[string]string{
				gitCommandKey("branch", "--show-current"):                                              "feature\n",
				gitCommandKey("reflog", "show", "--format=%H%x00%gs", "refs/heads/feature"):            "bbbbbbbbbbbbbbbb\x00commit\naaaaaaaaaaaaaaaa\x00branch: Created from main\n",
				gitCommandKey("branch", "--format=%(refname:short)", "--contains", "aaaaaaaaaaaaaaaa"): "feature\nmain\nrelease\n",
			},
		},
	}

	base, err := tool.determineComparisonBase(context.Background(), "/tmp/repo")
	require.NoError(t, err)
	assert.Equal(t, comparisonBase{
		Branch:       "feature",
		ParentBranch: "main",
		Commit:       "aaaaaaaaaaaaaaaa",
		Mode:         comparisonBaseModeBranchPoint,
	}, base)
}

func TestDetermineComparisonBaseFailsWhenParentBranchIsAmbiguous(t *testing.T) {
	t.Parallel()

	tool := &toolCheckSpecConformance{
		git: fakeGitRunner{
			outputs: map[string]string{
				gitCommandKey("branch", "--show-current"):                                              "feature\n",
				gitCommandKey("reflog", "show", "--format=%H%x00%gs", "refs/heads/feature"):            "bbbbbbbbbbbbbbbb\x00commit\naaaaaaaaaaaaaaaa\x00branch: Created from HEAD\n",
				gitCommandKey("branch", "--format=%(refname:short)", "--contains", "aaaaaaaaaaaaaaaa"): "feature\nmain\nrelease\n",
			},
		},
	}

	_, err := tool.determineComparisonBase(context.Background(), "/tmp/repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous parent branch")
}

func TestParsePackageCheckResultMarksNoDiffIssuesLatent(t *testing.T) {
	t.Parallel()

	result, err := parsePackageCheckResult(`{"conforms":false,"nonconformances":[{"severity":"minor","latent":false,"message":"mismatch"}]}`, false)
	require.NoError(t, err)
	require.NotNil(t, result.Conforms)
	assert.False(t, *result.Conforms)
	require.Len(t, result.Nonconformances, 1)
	assert.True(t, result.Nonconformances[0].Latent)
}

func TestRunOnlyChangedChecksOnlyModifiedPackagesAndStoresCAS(t *testing.T) {
	moduleDir := setupModuleRepo(t)
	writeFile(t, filepath.Join(moduleDir, "internal/foo/foo.go"), fooGoFile(`"foo changed"`))

	tool := NewCheckSpecConformanceTool(allowAllAuthorizer{sandboxDir: moduleDir}).(*toolCheckSpecConformance)
	var checked []string
	tool.runPackageCheck = func(ctx context.Context, req packageCheckRequest) (string, error) {
		checked = append(checked, req.Key)
		return `{"conforms":true}`, nil
	}

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-1",
		Name:   ToolNameCheckSpecConformance,
		Type:   "function_call",
		Input:  `{"only_changed":true}`,
	})
	require.False(t, result.IsError)

	var parsed map[string]packageCheckResult
	require.NoError(t, json.Unmarshal([]byte(result.Result), &parsed))
	require.Len(t, parsed, 1)
	assert.Equal(t, []string{"internal/foo"}, checked)
	require.Contains(t, parsed, "internal/foo")
	require.NotNil(t, parsed["internal/foo"].Conforms)
	assert.True(t, *parsed["internal/foo"].Conforms)

	mod, err := gocode.NewModule(moduleDir)
	require.NoError(t, err)
	fooPkg, err := mod.LoadPackageByRelativeDir("internal/foo")
	require.NoError(t, err)
	barPkg, err := mod.LoadPackageByRelativeDir("internal/bar")
	require.NoError(t, err)

	found, conforms, err := casconformance.Retrieve(newCASDB(moduleDir), fooPkg)
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, conforms)

	found, conforms, err = casconformance.Retrieve(newCASDB(moduleDir), barPkg)
	require.NoError(t, err)
	assert.False(t, found)
	assert.False(t, conforms)
}

func TestRunRecordsPerPackageErrorsWithoutFailingOverall(t *testing.T) {
	moduleDir := setupModuleRepo(t)

	tool := NewCheckSpecConformanceTool(allowAllAuthorizer{sandboxDir: moduleDir}).(*toolCheckSpecConformance)
	tool.runPackageCheck = func(ctx context.Context, req packageCheckRequest) (string, error) {
		if req.Key == "internal/foo" {
			return `{"conforms":true}`, nil
		}
		return `not json`, nil
	}

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-2",
		Name:   ToolNameCheckSpecConformance,
		Type:   "function_call",
		Input:  `{"only_changed":false}`,
	})
	require.False(t, result.IsError)

	var parsed map[string]packageCheckResult
	require.NoError(t, json.Unmarshal([]byte(result.Result), &parsed))
	require.Len(t, parsed, 2)
	require.Contains(t, parsed, "internal/foo")
	require.Contains(t, parsed, "internal/bar")
	require.NotNil(t, parsed["internal/foo"].Conforms)
	assert.True(t, *parsed["internal/foo"].Conforms)
	assert.Contains(t, parsed["internal/bar"].Error, "non-JSON")

	mod, err := gocode.NewModule(moduleDir)
	require.NoError(t, err)
	fooPkg, err := mod.LoadPackageByRelativeDir("internal/foo")
	require.NoError(t, err)
	barPkg, err := mod.LoadPackageByRelativeDir("internal/bar")
	require.NoError(t, err)

	found, conforms, err := casconformance.Retrieve(newCASDB(moduleDir), fooPkg)
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, conforms)

	found, conforms, err = casconformance.Retrieve(newCASDB(moduleDir), barPkg)
	require.NoError(t, err)
	assert.False(t, found)
	assert.False(t, conforms)
}

type fakeGitRunner struct {
	outputs map[string]string
}

func (f fakeGitRunner) Output(ctx context.Context, repoAbsDir string, args ...string) (string, error) {
	return f.outputs[gitCommandKey(args...)], nil
}

func gitCommandKey(args ...string) string {
	return strings.Join(args, "\x00")
}

type allowAllAuthorizer struct {
	sandboxDir string
}

func (a allowAllAuthorizer) SandboxDir() string {
	return a.sandboxDir
}

func (a allowAllAuthorizer) CodeUnitDir() string {
	return ""
}

func (a allowAllAuthorizer) IsCodeUnitDomain() bool {
	return false
}

func (a allowAllAuthorizer) WithoutCodeUnit() authdomain.Authorizer {
	return a
}

func (a allowAllAuthorizer) IsAuthorizedForRead(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	return nil
}

func (a allowAllAuthorizer) IsAuthorizedForWrite(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	return nil
}

func (a allowAllAuthorizer) IsShellAuthorized(requestPermission bool, requestReason string, cwd string, command []string) error {
	return nil
}

func (a allowAllAuthorizer) Close() {}

func setupModuleRepo(t *testing.T) string {
	t.Helper()

	moduleDir := t.TempDir()
	writeFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com/specmod\n\ngo 1.24.4\n")
	writeFile(t, filepath.Join(moduleDir, "internal/foo/foo.go"), fooGoFile(`"foo"`))
	writeFile(t, filepath.Join(moduleDir, "internal/foo/SPEC.md"), fooSpec())
	writeFile(t, filepath.Join(moduleDir, "internal/bar/bar.go"), barGoFile(`"bar"`))
	writeFile(t, filepath.Join(moduleDir, "internal/bar/SPEC.md"), barSpec())

	runGit(t, moduleDir, "init")
	runGit(t, moduleDir, "config", "user.name", "Test User")
	runGit(t, moduleDir, "config", "user.email", "test@example.com")
	runGit(t, moduleDir, "checkout", "-b", "main")
	runGit(t, moduleDir, "add", ".")
	runGit(t, moduleDir, "commit", "-m", "initial")
	return moduleDir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func writeFile(t *testing.T, absPath string, contents string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(absPath), 0o755))
	require.NoError(t, os.WriteFile(absPath, []byte(contents), 0o644))
}

func fooGoFile(returnValue string) string {
	return "package foo\n\n// Foo returns foo.\nfunc Foo() string {\n\treturn " + returnValue + "\n}\n"
}

func barGoFile(returnValue string) string {
	return "package bar\n\n// Bar returns bar.\nfunc Bar() string {\n\treturn " + returnValue + "\n}\n"
}

func fooSpec() string {
	return "# foo\n\n## Public API\n\n```go\n// Foo returns foo.\nfunc Foo() string\n```\n"
}

func barSpec() string {
	return "# bar\n\n## Public API\n\n```go\n// Bar returns bar.\nfunc Bar() string\n```\n"
}
