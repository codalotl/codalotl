package gittools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeuristicMergeBaseSimpleFeatureBranch(t *testing.T) {
	t.Parallel()

	repoDir := newTestRepo(t)
	baseCommit := commitFile(t, repoDir, "base.txt", "base\n", "base commit")

	git(t, repoDir, "checkout", "-b", "my-feature-branch")
	commitFile(t, repoDir, "feature.txt", "feature\n", "feature commit")

	commit, ref, err := HeuristicMergeBase(repoDir)
	require.NoError(t, err)
	assert.Equal(t, baseCommit, commit)
	assert.Equal(t, "main", ref)
}

func TestHeuristicMergeBaseAfterMergingMainIntoFeature(t *testing.T) {
	t.Parallel()

	repoDir := newTestRepo(t)
	commitFile(t, repoDir, "base.txt", "base\n", "base commit")

	git(t, repoDir, "checkout", "-b", "my-feature-branch")
	commitFile(t, repoDir, "feature.txt", "feature\n", "feature commit")

	git(t, repoDir, "checkout", "main")
	mainCommit := commitFile(t, repoDir, "main.txt", "main\n", "main commit")

	git(t, repoDir, "checkout", "my-feature-branch")
	git(t, repoDir, "merge", "--no-ff", "-m", "merge main", "main")

	commit, ref, err := HeuristicMergeBase(repoDir)
	require.NoError(t, err)
	assert.Equal(t, mainCommit, commit)
	assert.Equal(t, "main", ref)
}

func TestHeuristicMergeBaseCanonicalizesLocalAndRemoteAliases(t *testing.T) {
	t.Parallel()

	repoDir := newTestRepo(t)
	baseCommit := commitFile(t, repoDir, "base.txt", "base\n", "base commit")

	remoteDir := filepath.Join(t.TempDir(), "origin.git")
	git(t, "", "init", "--bare", remoteDir)
	git(t, repoDir, "remote", "add", "origin", remoteDir)
	git(t, repoDir, "push", "-u", "origin", "main")

	git(t, repoDir, "checkout", "-b", "my-feature-branch")
	commitFile(t, repoDir, "feature.txt", "feature\n", "feature commit")
	git(t, repoDir, "push", "-u", "origin", "my-feature-branch")
	git(t, repoDir, "fetch", "origin")

	commit, ref, err := HeuristicMergeBase(repoDir)
	require.NoError(t, err)
	assert.Equal(t, baseCommit, commit)
	assert.Equal(t, "main", ref)
}

func TestHeuristicMergeBaseUsesTrackedRemoteBaseWhenNoLocalAliasExists(t *testing.T) {
	t.Parallel()

	repoDir := newTestRepo(t)
	baseCommit := commitFile(t, repoDir, "base.txt", "base\n", "base commit")

	remoteDir := filepath.Join(t.TempDir(), "origin.git")
	git(t, "", "init", "--bare", remoteDir)
	git(t, repoDir, "remote", "add", "origin", remoteDir)
	git(t, repoDir, "push", "-u", "origin", "main")

	git(t, repoDir, "checkout", "--detach")
	git(t, repoDir, "branch", "-D", "main")
	git(t, repoDir, "checkout", "-b", "my-feature-branch", "origin/main")
	commitFile(t, repoDir, "feature.txt", "feature\n", "feature commit")

	commit, ref, err := HeuristicMergeBase(repoDir)
	require.NoError(t, err)
	assert.Equal(t, baseCommit, commit)
	assert.Equal(t, "origin/main", ref)
}

func TestChangedPathsSinceCommittedOnly(t *testing.T) {
	t.Parallel()

	repoDir := newTestRepo(t)
	baseCommit := commitFile(t, repoDir, "base.txt", "base\n", "base commit")

	git(t, repoDir, "checkout", "-b", "my-feature-branch")
	commitFile(t, repoDir, "pkg/z.go", "package pkg\n", "add z")
	commitFile(t, repoDir, "pkg/a.go", "package pkg\n", "add a")

	paths, err := ChangedPathsSince(repoDir, baseCommit, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"pkg/a.go", "pkg/z.go"}, paths)
}

func TestChangedPathsSinceOptionallyIncludesUncommittedChanges(t *testing.T) {
	t.Parallel()

	repoDir := newTestRepo(t)
	baseCommit := commitFile(t, repoDir, "base.txt", "base\n", "base commit")

	git(t, repoDir, "checkout", "-b", "my-feature-branch")
	commitFile(t, repoDir, "committed.txt", "committed\n", "committed change")

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "staged.txt"), []byte("staged\n"), 0o644))
	git(t, repoDir, "add", "staged.txt")
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "base.txt"), []byte("base updated\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "untracked.txt"), []byte("untracked\n"), 0o644))

	committedPaths, err := ChangedPathsSince(repoDir, baseCommit, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"committed.txt"}, committedPaths)

	allPaths, err := ChangedPathsSince(repoDir, baseCommit, true)
	require.NoError(t, err)
	assert.Equal(t, []string{"base.txt", "committed.txt", "staged.txt", "untracked.txt"}, allPaths)
}

func TestChangedPathsSinceIncludesDeletedAndRenamedPaths(t *testing.T) {
	t.Parallel()

	repoDir := newTestRepo(t)
	commitFile(t, repoDir, "old/file.txt", "contents\n", "add old file")
	baseCommit := commitFile(t, repoDir, "deleted.txt", "delete me\n", "add deleted file")

	git(t, repoDir, "checkout", "-b", "my-feature-branch")
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "new"), 0o755))
	require.NoError(t, os.Rename(filepath.Join(repoDir, "old/file.txt"), filepath.Join(repoDir, "new/file.txt")))
	require.NoError(t, os.Remove(filepath.Join(repoDir, "deleted.txt")))
	git(t, repoDir, "add", "-A")
	git(t, repoDir, "commit", "-m", "rename and delete")

	paths, err := ChangedPathsSince(repoDir, baseCommit, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"deleted.txt", "new/file.txt", "old/file.txt"}, paths)
}

func newTestRepo(t *testing.T) string {
	t.Helper()

	repoDir := t.TempDir()
	git(t, "", "init", "--initial-branch=main", repoDir)
	git(t, repoDir, "config", "user.name", "Test User")
	git(t, repoDir, "config", "user.email", "test@example.com")
	return repoDir
}

func commitFile(t *testing.T, repoDir, path, contents, message string) string {
	t.Helper()

	fullPath := filepath.Join(repoDir, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, []byte(contents), 0o644))
	git(t, repoDir, "add", path)
	git(t, repoDir, "commit", "-m", message)
	return git(t, repoDir, "rev-parse", "HEAD")
}

func git(t *testing.T, repoDir string, args ...string) string {
	t.Helper()

	out, err := gitOutput(repoDir, args...)
	require.NoError(t, err)
	return trimGitOutput(out)
}

func trimGitOutput(out string) string {
	return strings.TrimSpace(out)
}
