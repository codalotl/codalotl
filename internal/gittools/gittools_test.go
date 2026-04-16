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
