package integration

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepoDirForCaseWithoutRepoUsesFixtureRepoPath(t *testing.T) {
	caseDir := t.TempDir()

	want, err := fixtureRepoPath()
	require.NoError(t, err)

	got, err := repoDirForCase(caseDir)
	require.NoError(t, err)

	assert.Equal(t, want, got)
	assert.True(t, filepath.IsAbs(got))
}

func TestIsFixtureRepoPath(t *testing.T) {
	fixturePath, err := fixtureRepoPath()
	require.NoError(t, err)

	got, err := isFixtureRepoPath(fixturePath)
	require.NoError(t, err)
	assert.True(t, got)

	got, err = isFixtureRepoPath(t.TempDir())
	require.NoError(t, err)
	assert.False(t, got)
}

func TestMatchesTextMatcherRequiresOrderedTexts(t *testing.T) {
	assert.True(t, matchesTextMatcher(map[string]any{
		"match": "partial",
		"texts": []any{
			"<apply-patch ok=\"true\">",
			"$ golangci-lint run ./...",
			"$ go test ./...",
		},
	}, "<apply-patch ok=\"true\">\n$ golangci-lint run ./...\n$ go test ./...\n</apply-patch>", nil))

	assert.False(t, matchesTextMatcher(map[string]any{
		"match": "partial",
		"texts": []any{
			"<apply-patch ok=\"true\">",
			"$ golangci-lint run ./...",
			"$ go test ./...",
		},
	}, "<apply-patch ok=\"true\">\n$ go test ./...\n</apply-patch>", nil))

	assert.False(t, matchesTextMatcher(map[string]any{
		"match": "partial",
		"texts": []any{
			"$ golangci-lint run ./...",
			"<apply-patch ok=\"true\">",
		},
	}, "<apply-patch ok=\"true\">\n$ golangci-lint run ./...\n$ go test ./...\n</apply-patch>", nil))
}

func TestAssertEventSubsequenceNormalizesRuntimePaths(t *testing.T) {
	workDir := filepath.Join(string(filepath.Separator), "tmp", "case-root")

	err := assertEventSubsequence([]map[string]any{
		{
			"type":    "assistant_text",
			"content": "Updated catalog/query.go successfully.",
		},
	}, []map[string]any{
		{
			"type":    "assistant_text",
			"content": "Updated " + filepath.Join(workDir, "catalog", "query.go") + " successfully.",
		},
	}, []string{workDir})

	require.NoError(t, err)
}
