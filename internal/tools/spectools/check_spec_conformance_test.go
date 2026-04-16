package spectools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/codalotl/codalotl/internal/gocas/casconformance"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetermineComparisonBaseUsesHeuristicBaseOnMainBranch(t *testing.T) {
	t.Parallel()

	tool := &toolCheckSpecConformance{
		git: fakeGitRunner{
			outputs: map[string]string{
				gitCommandKey("branch", "--show-current"): "main\n",
			},
		},
		heuristicBase: func(repoDir string) (string, string, error) {
			assert.Equal(t, "/tmp/repo", repoDir)
			return "0123456789abcdef", "feature", nil
		},
	}

	base, err := tool.determineComparisonBase(context.Background(), "/tmp/repo")
	require.NoError(t, err)
	assert.Equal(t, comparisonBase{
		Branch:       "main",
		ParentBranch: "feature",
		Commit:       "0123456789abcdef",
	}, base)
}

func TestDetermineComparisonBaseUsesHeuristicBaseForFeatureBranch(t *testing.T) {
	t.Parallel()

	tool := &toolCheckSpecConformance{
		git: fakeGitRunner{
			outputs: map[string]string{
				gitCommandKey("branch", "--show-current"): "feature\n",
			},
		},
		heuristicBase: func(repoDir string) (string, string, error) {
			assert.Equal(t, "/tmp/repo", repoDir)
			return "cccccccccccccccc", "main", nil
		},
	}

	base, err := tool.determineComparisonBase(context.Background(), "/tmp/repo")
	require.NoError(t, err)
	assert.Equal(t, comparisonBase{
		Branch:       "feature",
		ParentBranch: "main",
		Commit:       "cccccccccccccccc",
	}, base)
}

func TestDetermineComparisonBaseFailsWhenHeuristicBaseReturnsEmptyCommit(t *testing.T) {
	t.Parallel()

	tool := &toolCheckSpecConformance{
		git: fakeGitRunner{
			outputs: map[string]string{
				gitCommandKey("branch", "--show-current"): "feature\n",
			},
		},
		heuristicBase: func(repoDir string) (string, string, error) {
			return "", "main", nil
		},
	}

	_, err := tool.determineComparisonBase(context.Background(), "/tmp/repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty commit")
}

func TestCollectRepoChangesUsesChangedPathsAndSeparatesUntracked(t *testing.T) {
	t.Parallel()

	tool := &toolCheckSpecConformance{
		git: fakeGitRunner{
			outputs: map[string]string{
				gitCommandKey("ls-files", "--others", "--exclude-standard"): "internal/foo/new.txt\nscratch.md\n",
			},
		},
		changedPaths: func(repoDir string, baseCommit string, includeUncommitted bool) ([]string, error) {
			assert.Equal(t, "/tmp/repo", repoDir)
			assert.Equal(t, "0123456789abcdef", baseCommit)
			assert.True(t, includeUncommitted)
			return []string{
				"internal/bar/bar.go",
				"internal/foo/foo.go",
				"internal/foo/new.txt",
			}, nil
		},
	}

	changes, err := tool.collectRepoChanges(context.Background(), "/tmp/repo", "0123456789abcdef")
	require.NoError(t, err)
	assert.Equal(t, []string{"internal/bar/bar.go", "internal/foo/foo.go"}, changes.tracked)
	assert.Equal(t, []string{"internal/foo/new.txt"}, changes.untracked)
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

func TestNewPackageModeCodeUnitIncludesReachableTestdataAndExcludesNestedPackages(t *testing.T) {
	t.Parallel()

	pkgDir := t.TempDir()
	writeFile(t, filepath.Join(pkgDir, "foo.go"), fooGoFile(`"foo"`))
	writeFile(t, filepath.Join(pkgDir, "README.md"), "# foo\n")
	writeFile(t, filepath.Join(pkgDir, "support/config.json"), "{}\n")
	writeFile(t, filepath.Join(pkgDir, "testdata/fixture.go"), "package fixture\n")
	writeFile(t, filepath.Join(pkgDir, "child/child.go"), childGoFile(`"child"`))
	writeFile(t, filepath.Join(pkgDir, "child/notes.txt"), "nested support\n")
	writeFile(t, filepath.Join(pkgDir, "child/testdata/input.txt"), "nested fixture\n")

	unit, err := newPackageModeCodeUnit("package example.com/foo", pkgDir)
	require.NoError(t, err)

	assert.True(t, unit.Includes(filepath.Join(pkgDir, "README.md")))
	assert.True(t, unit.Includes(filepath.Join(pkgDir, "support/config.json")))
	assert.True(t, unit.Includes(filepath.Join(pkgDir, "testdata/fixture.go")))
	assert.False(t, unit.Includes(filepath.Join(pkgDir, "child/child.go")))
	assert.False(t, unit.Includes(filepath.Join(pkgDir, "child/notes.txt")))
	assert.False(t, unit.Includes(filepath.Join(pkgDir, "child/testdata/input.txt")))
}

func TestRunOnlyChangedChecksOnlyModifiedPackagesAndStoresCAS(t *testing.T) {
	moduleDir := setupModuleRepo(t)
	writeFile(t, filepath.Join(moduleDir, "internal/foo/foo.go"), fooGoFile(`"foo changed"`))

	tool := NewCheckSpecConformanceTool(allowAllAuthorizer{sandboxDir: moduleDir}).(*toolCheckSpecConformance)
	recorder := &checkedRecorder{}
	tool.runPackageCheck = func(ctx context.Context, req packageCheckRequest) (string, error) {
		recorder.add(req.Key)
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
	assert.ElementsMatch(t, []string{"internal/foo"}, recorder.list())
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

func TestRunOnlyChangedChecksCommittedFeatureBranchChangesAgainstComparisonBase(t *testing.T) {
	moduleDir := setupModuleRepo(t)
	runGit(t, moduleDir, "checkout", "-b", "feature")

	writeFile(t, filepath.Join(moduleDir, "internal/foo/foo.go"), fooGoFile(`"foo changed"`))
	runGit(t, moduleDir, "add", "internal/foo/foo.go")
	runGit(t, moduleDir, "commit", "-m", "change foo")

	tool := NewCheckSpecConformanceTool(allowAllAuthorizer{sandboxDir: moduleDir}).(*toolCheckSpecConformance)
	recorder := &checkedRecorder{}
	tool.runPackageCheck = func(ctx context.Context, req packageCheckRequest) (string, error) {
		recorder.add(req.Key)
		return `{"conforms":true}`, nil
	}

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-feature-branch",
		Name:   ToolNameCheckSpecConformance,
		Type:   "function_call",
		Input:  `{"only_changed":true}`,
	})
	require.False(t, result.IsError)

	var parsed map[string]packageCheckResult
	require.NoError(t, json.Unmarshal([]byte(result.Result), &parsed))
	require.Len(t, parsed, 1)
	assert.ElementsMatch(t, []string{"internal/foo"}, recorder.list())
	require.Contains(t, parsed, "internal/foo")
	assert.NotContains(t, parsed, "internal/bar")
}

func TestRunRechecksChangedCASVerifiedPackage(t *testing.T) {
	moduleDir := setupModuleRepo(t)

	tool := NewCheckSpecConformanceTool(allowAllAuthorizer{sandboxDir: moduleDir}).(*toolCheckSpecConformance)
	mod, err := gocode.NewModule(moduleDir)
	require.NoError(t, err)
	fooPkg, err := mod.LoadPackageByRelativeDir("internal/foo")
	require.NoError(t, err)
	barPkg, err := mod.LoadPackageByRelativeDir("internal/bar")
	require.NoError(t, err)
	require.NoError(t, tool.storeConformanceState(fooPkg))
	require.NoError(t, tool.storeConformanceState(barPkg))

	writeFile(t, filepath.Join(moduleDir, "internal/foo/foo.go"), fooGoFile(`"foo changed"`))

	recorder := &checkedRecorder{}
	tool.runPackageCheck = func(ctx context.Context, req packageCheckRequest) (string, error) {
		recorder.add(req.Key)
		return `{"conforms":true}`, nil
	}

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-recheck-cas-verified",
		Name:   ToolNameCheckSpecConformance,
		Type:   "function_call",
		Input:  `{"only_changed":false}`,
	})
	require.False(t, result.IsError)

	var parsed map[string]packageCheckResult
	require.NoError(t, json.Unmarshal([]byte(result.Result), &parsed))
	require.Len(t, parsed, 1)
	assert.ElementsMatch(t, []string{"internal/foo"}, recorder.list())
	require.Contains(t, parsed, "internal/foo")
	assert.NotContains(t, parsed, "internal/bar")
}

func TestRunOnlyChangedRechecksCASVerifiedPackageWhenSupportFileChanges(t *testing.T) {
	moduleDir := setupModuleRepo(t)

	tool := NewCheckSpecConformanceTool(allowAllAuthorizer{sandboxDir: moduleDir}).(*toolCheckSpecConformance)
	mod, err := gocode.NewModule(moduleDir)
	require.NoError(t, err)
	fooPkg, err := mod.LoadPackageByRelativeDir("internal/foo")
	require.NoError(t, err)
	require.NoError(t, tool.storeConformanceState(fooPkg))

	writeFile(t, filepath.Join(moduleDir, "internal/foo/testdata/input.txt"), "changed fixture\n")

	recorder := &checkedRecorder{}
	tool.runPackageCheck = func(ctx context.Context, req packageCheckRequest) (string, error) {
		recorder.add(req.Key)
		return `{"conforms":true}`, nil
	}

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-support-file",
		Name:   ToolNameCheckSpecConformance,
		Type:   "function_call",
		Input:  `{"only_changed":true}`,
	})
	require.False(t, result.IsError)

	var parsed map[string]packageCheckResult
	require.NoError(t, json.Unmarshal([]byte(result.Result), &parsed))
	require.Len(t, parsed, 1)
	assert.ElementsMatch(t, []string{"internal/foo"}, recorder.list())
	require.Contains(t, parsed, "internal/foo")
}

func TestRunOnlyChangedRechecksCASVerifiedPackageWhenReachableTestdataGoFileChanges(t *testing.T) {
	moduleDir := setupModuleRepo(t)
	writeFile(t, filepath.Join(moduleDir, "internal/foo/testdata/fixture.go"), "package fixture\n")
	runGit(t, moduleDir, "add", ".")
	runGit(t, moduleDir, "commit", "-m", "add foo go fixture")

	tool := NewCheckSpecConformanceTool(allowAllAuthorizer{sandboxDir: moduleDir}).(*toolCheckSpecConformance)
	mod, err := gocode.NewModule(moduleDir)
	require.NoError(t, err)
	fooPkg, err := mod.LoadPackageByRelativeDir("internal/foo")
	require.NoError(t, err)
	require.NoError(t, tool.storeConformanceState(fooPkg))

	writeFile(t, filepath.Join(moduleDir, "internal/foo/testdata/fixture.go"), "package fixture\n\nconst Changed = true\n")

	recorder := &checkedRecorder{}
	tool.runPackageCheck = func(ctx context.Context, req packageCheckRequest) (string, error) {
		recorder.add(req.Key)
		return `{"conforms":true}`, nil
	}

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-testdata-go-change",
		Name:   ToolNameCheckSpecConformance,
		Type:   "function_call",
		Input:  `{"only_changed":true}`,
	})
	require.False(t, result.IsError)

	var parsed map[string]packageCheckResult
	require.NoError(t, json.Unmarshal([]byte(result.Result), &parsed))
	require.Len(t, parsed, 1)
	assert.ElementsMatch(t, []string{"internal/foo"}, recorder.list())
	require.Contains(t, parsed, "internal/foo")
}

func TestRunOnlyChangedRechecksCASVerifiedPackageWhenReachableTestdataGoFileIsDeleted(t *testing.T) {
	moduleDir := setupModuleRepo(t)
	writeFile(t, filepath.Join(moduleDir, "internal/foo/testdata/fixture.go"), "package fixture\n")
	runGit(t, moduleDir, "add", ".")
	runGit(t, moduleDir, "commit", "-m", "add foo go fixture")

	tool := NewCheckSpecConformanceTool(allowAllAuthorizer{sandboxDir: moduleDir}).(*toolCheckSpecConformance)
	mod, err := gocode.NewModule(moduleDir)
	require.NoError(t, err)
	fooPkg, err := mod.LoadPackageByRelativeDir("internal/foo")
	require.NoError(t, err)
	require.NoError(t, tool.storeConformanceState(fooPkg))

	require.NoError(t, os.Remove(filepath.Join(moduleDir, "internal/foo/testdata/fixture.go")))

	recorder := &checkedRecorder{}
	tool.runPackageCheck = func(ctx context.Context, req packageCheckRequest) (string, error) {
		recorder.add(req.Key)
		return `{"conforms":true}`, nil
	}

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-testdata-go-delete",
		Name:   ToolNameCheckSpecConformance,
		Type:   "function_call",
		Input:  `{"only_changed":true}`,
	})
	require.False(t, result.IsError)

	var parsed map[string]packageCheckResult
	require.NoError(t, json.Unmarshal([]byte(result.Result), &parsed))
	require.Len(t, parsed, 1)
	assert.ElementsMatch(t, []string{"internal/foo"}, recorder.list())
	require.Contains(t, parsed, "internal/foo")
}

func TestRunOnlyChangedRechecksCASVerifiedPackageWhenSupportSubtreeIsDeleted(t *testing.T) {
	moduleDir := setupModuleRepo(t)
	writeFile(t, filepath.Join(moduleDir, "internal/foo/testdata/golden/input.txt"), "fixture\n")
	runGit(t, moduleDir, "add", ".")
	runGit(t, moduleDir, "commit", "-m", "add foo fixture")

	tool := NewCheckSpecConformanceTool(allowAllAuthorizer{sandboxDir: moduleDir}).(*toolCheckSpecConformance)
	mod, err := gocode.NewModule(moduleDir)
	require.NoError(t, err)
	fooPkg, err := mod.LoadPackageByRelativeDir("internal/foo")
	require.NoError(t, err)
	require.NoError(t, tool.storeConformanceState(fooPkg))

	require.NoError(t, os.RemoveAll(filepath.Join(moduleDir, "internal/foo/testdata")))

	recorder := &checkedRecorder{}
	tool.runPackageCheck = func(ctx context.Context, req packageCheckRequest) (string, error) {
		recorder.add(req.Key)
		return `{"conforms":true}`, nil
	}

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-support-delete",
		Name:   ToolNameCheckSpecConformance,
		Type:   "function_call",
		Input:  `{"only_changed":true}`,
	})
	require.False(t, result.IsError)

	var parsed map[string]packageCheckResult
	require.NoError(t, json.Unmarshal([]byte(result.Result), &parsed))
	require.Len(t, parsed, 1)
	assert.ElementsMatch(t, []string{"internal/foo"}, recorder.list())
	require.Contains(t, parsed, "internal/foo")
}

func TestRunOnlyChangedRechecksCASVerifiedPackageWhenTrackedFileMovesOut(t *testing.T) {
	moduleDir := setupModuleRepo(t)
	writeFile(t, filepath.Join(moduleDir, "internal/foo/testdata/input.txt"), "fixture\n")
	runGit(t, moduleDir, "add", ".")
	runGit(t, moduleDir, "commit", "-m", "add foo fixture")

	tool := NewCheckSpecConformanceTool(allowAllAuthorizer{sandboxDir: moduleDir}).(*toolCheckSpecConformance)
	mod, err := gocode.NewModule(moduleDir)
	require.NoError(t, err)
	fooPkg, err := mod.LoadPackageByRelativeDir("internal/foo")
	require.NoError(t, err)
	require.NoError(t, tool.storeConformanceState(fooPkg))

	require.NoError(t, os.MkdirAll(filepath.Join(moduleDir, "internal/bar/testdata"), 0o755))
	require.NoError(t, os.Rename(
		filepath.Join(moduleDir, "internal/foo/testdata/input.txt"),
		filepath.Join(moduleDir, "internal/bar/testdata/input.txt"),
	))

	recorder := &checkedRecorder{}
	tool.runPackageCheck = func(ctx context.Context, req packageCheckRequest) (string, error) {
		recorder.add(req.Key)
		return `{"conforms":true}`, nil
	}

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-support-move-out",
		Name:   ToolNameCheckSpecConformance,
		Type:   "function_call",
		Input:  `{"only_changed":true}`,
	})
	require.False(t, result.IsError)

	var parsed map[string]packageCheckResult
	require.NoError(t, json.Unmarshal([]byte(result.Result), &parsed))
	require.Len(t, parsed, 2)
	assert.ElementsMatch(t, []string{"internal/bar", "internal/foo"}, recorder.list())
	require.Contains(t, parsed, "internal/foo")
	require.Contains(t, parsed, "internal/bar")
}

func TestRunOnlyChangedDoesNotAttributeDescendantPackageChangesToParent(t *testing.T) {
	moduleDir := setupModuleRepo(t)

	writeFile(t, filepath.Join(moduleDir, "internal/foo/child/child.go"), childGoFile(`"child"`))
	writeFile(t, filepath.Join(moduleDir, "internal/foo/child/SPEC.md"), childSpec())

	tool := NewCheckSpecConformanceTool(allowAllAuthorizer{sandboxDir: moduleDir}).(*toolCheckSpecConformance)
	recorder := &checkedRecorder{}
	tool.runPackageCheck = func(ctx context.Context, req packageCheckRequest) (string, error) {
		recorder.add(req.Key)
		return `{"conforms":true}`, nil
	}

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-descendant",
		Name:   ToolNameCheckSpecConformance,
		Type:   "function_call",
		Input:  `{"only_changed":true}`,
	})
	require.False(t, result.IsError)

	var parsed map[string]packageCheckResult
	require.NoError(t, json.Unmarshal([]byte(result.Result), &parsed))
	require.Len(t, parsed, 1)
	assert.ElementsMatch(t, []string{"internal/foo/child"}, recorder.list())
	require.Contains(t, parsed, "internal/foo/child")
	assert.NotContains(t, parsed, "internal/foo")
}

func TestRunOnlyChangedDoesNotAttributeDeletedDescendantPackagePathsToParent(t *testing.T) {
	moduleDir := setupModuleRepo(t)

	writeFile(t, filepath.Join(moduleDir, "internal/foo/child/child.go"), childGoFile(`"child"`))
	writeFile(t, filepath.Join(moduleDir, "internal/foo/child/SPEC.md"), childSpec())
	runGit(t, moduleDir, "add", ".")
	runGit(t, moduleDir, "commit", "-m", "add child package")

	require.NoError(t, os.RemoveAll(filepath.Join(moduleDir, "internal/foo/child")))

	tool := NewCheckSpecConformanceTool(allowAllAuthorizer{sandboxDir: moduleDir}).(*toolCheckSpecConformance)
	recorder := &checkedRecorder{}
	tool.runPackageCheck = func(ctx context.Context, req packageCheckRequest) (string, error) {
		recorder.add(req.Key)
		return `{"conforms":true}`, nil
	}

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-descendant-delete",
		Name:   ToolNameCheckSpecConformance,
		Type:   "function_call",
		Input:  `{"only_changed":true}`,
	})
	require.False(t, result.IsError)

	var parsed map[string]packageCheckResult
	require.NoError(t, json.Unmarshal([]byte(result.Result), &parsed))
	require.Empty(t, parsed)
	assert.Empty(t, recorder.list())
}

func TestRunOnlyChangedDoesNotTreatRootPackageAsWholeRepo(t *testing.T) {
	moduleDir := setupModuleRepo(t)

	writeFile(t, filepath.Join(moduleDir, "root.go"), rootGoFile(`"root"`))
	writeFile(t, filepath.Join(moduleDir, "SPEC.md"), rootSpec())
	runGit(t, moduleDir, "add", ".")
	runGit(t, moduleDir, "commit", "-m", "add root package")
	runGit(t, moduleDir, "branch", "after-root")

	writeFile(t, filepath.Join(moduleDir, "internal/foo/foo.go"), fooGoFile(`"foo changed"`))

	tool := NewCheckSpecConformanceTool(allowAllAuthorizer{sandboxDir: moduleDir}).(*toolCheckSpecConformance)
	recorder := &checkedRecorder{}
	tool.runPackageCheck = func(ctx context.Context, req packageCheckRequest) (string, error) {
		recorder.add(req.Key)
		return `{"conforms":true}`, nil
	}

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-root-scope",
		Name:   ToolNameCheckSpecConformance,
		Type:   "function_call",
		Input:  `{"only_changed":true}`,
	})
	require.False(t, result.IsError)

	var parsed map[string]packageCheckResult
	require.NoError(t, json.Unmarshal([]byte(result.Result), &parsed))
	require.Len(t, parsed, 1)
	assert.ElementsMatch(t, []string{"internal/foo"}, recorder.list())
	require.Contains(t, parsed, "internal/foo")
	assert.NotContains(t, parsed, ".")
}

func TestRunUsesCurrentModulePackagesOnly(t *testing.T) {
	moduleDir := setupModuleRepo(t)

	writeFile(t, filepath.Join(moduleDir, "third_party/nested/go.mod"), "module example.com/nested\n\ngo 1.24.4\n")
	writeFile(t, filepath.Join(moduleDir, "third_party/nested/nested.go"), nestedGoFile(`"nested"`))
	writeFile(t, filepath.Join(moduleDir, "third_party/nested/SPEC.md"), nestedSpec())

	tool := NewCheckSpecConformanceTool(allowAllAuthorizer{sandboxDir: moduleDir}).(*toolCheckSpecConformance)
	recorder := &checkedRecorder{}
	tool.runPackageCheck = func(ctx context.Context, req packageCheckRequest) (string, error) {
		recorder.add(req.Key)
		return `{"conforms":true}`, nil
	}

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-current-module",
		Name:   ToolNameCheckSpecConformance,
		Type:   "function_call",
		Input:  `{"only_changed":false}`,
	})
	require.False(t, result.IsError)

	var parsed map[string]packageCheckResult
	require.NoError(t, json.Unmarshal([]byte(result.Result), &parsed))
	require.Len(t, parsed, 2)
	assert.ElementsMatch(t, []string{"internal/bar", "internal/foo"}, recorder.list())
	assert.NotContains(t, parsed, "third_party/nested")
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

func TestRunRecordsPackagePreparationFailuresWithoutFailingOverall(t *testing.T) {
	moduleDir := setupModuleRepo(t)

	tool := NewCheckSpecConformanceTool(allowAllAuthorizer{sandboxDir: moduleDir}).(*toolCheckSpecConformance)
	tool.specDiffContext = func(pkg *gocode.Package) (string, error) {
		if pkg.RelativeDir == "internal/bar" {
			return "", errors.New("spec diff exploded")
		}
		return "", nil
	}
	tool.runPackageCheck = func(ctx context.Context, req packageCheckRequest) (string, error) {
		return `{"conforms":true}`, nil
	}

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-package-prep-error",
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
	assert.Contains(t, parsed["internal/bar"].Error, "compute spec diff: spec diff exploded")

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

func TestRunRecordsPackageCASWriteFailuresWithoutFailingOverall(t *testing.T) {
	moduleDir := setupModuleRepo(t)

	mod, err := gocode.NewModule(moduleDir)
	require.NoError(t, err)
	barPkg, err := mod.LoadPackageByRelativeDir("internal/bar")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(moduleDir, ".codalotl", "cas"), 0o755))
	require.NoError(t, casconformance.Store(newCASDB(moduleDir), barPkg, true))

	tool := NewCheckSpecConformanceTool(denyWritesAuthorizer{
		allowAllAuthorizer: allowAllAuthorizer{sandboxDir: moduleDir},
		err:                errors.New("writes disabled"),
	}).(*toolCheckSpecConformance)
	tool.runPackageCheck = func(ctx context.Context, req packageCheckRequest) (string, error) {
		return `{"conforms":true}`, nil
	}

	result := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-package-cas-write-error",
		Name:   ToolNameCheckSpecConformance,
		Type:   "function_call",
		Input:  `{"only_changed":false}`,
	})
	require.False(t, result.IsError)

	var parsed map[string]packageCheckResult
	require.NoError(t, json.Unmarshal([]byte(result.Result), &parsed))
	require.Len(t, parsed, 1)
	require.Contains(t, parsed, "internal/foo")
	assert.Contains(t, parsed["internal/foo"].Error, "store CAS conformance: writes disabled")
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

type checkedRecorder struct {
	mu      sync.Mutex
	checked []string
}

func (r *checkedRecorder) add(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.checked = append(r.checked, key)
}

func (r *checkedRecorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.checked...)
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

type denyWritesAuthorizer struct {
	allowAllAuthorizer
	err error
}

func (a denyWritesAuthorizer) IsAuthorizedForWrite(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	return a.err
}

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
	runGit(t, moduleDir, "branch", "gittools-base")
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

func childGoFile(returnValue string) string {
	return "package child\n\n// Child returns child.\nfunc Child() string {\n\treturn " + returnValue + "\n}\n"
}

func rootGoFile(returnValue string) string {
	return "package specmod\n\n// Root returns root.\nfunc Root() string {\n\treturn " + returnValue + "\n}\n"
}

func nestedGoFile(returnValue string) string {
	return "package nested\n\n// Nested returns nested.\nfunc Nested() string {\n\treturn " + returnValue + "\n}\n"
}

func fooSpec() string {
	return "# foo\n\n## Public API\n\n```go\n// Foo returns foo.\nfunc Foo() string\n```\n"
}

func barSpec() string {
	return "# bar\n\n## Public API\n\n```go\n// Bar returns bar.\nfunc Bar() string\n```\n"
}

func childSpec() string {
	return "# child\n\n## Public API\n\n```go\n// Child returns child.\nfunc Child() string\n```\n"
}

func rootSpec() string {
	return "# specmod\n\n## Public API\n\n```go\n// Root returns root.\nfunc Root() string\n```\n"
}

func nestedSpec() string {
	return "# nested\n\n## Public API\n\n```go\n// Nested returns nested.\nfunc Nested() string\n```\n"
}
