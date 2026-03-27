package integration

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/mockllm/mockopenai"
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

func TestAugmentReplayMockOpenAIErrorIncludesPrunedActualAndExpectedRequests(t *testing.T) {
	workDir := filepath.Join(string(filepath.Separator), "tmp", "case-root")
	handler, err := mockopenai.NewHandler([]byte(`{
		"responses": [
			{
				"name": "turn-01",
				"consume": true,
				"request": {
					"model": "mock-model-case-name",
					"input": [
						{
							"type": "message",
							"content": [
								{"type": "input_text"}
							]
						},
						{
							"type": "message",
							"content": [
								{"type": "input_text"}
							]
						},
						{
							"type": "message",
							"content": [
								{"type": "input_text", "text": "expected user message"}
							]
						}
					]
				},
				"response": {
					"id": "resp_1",
					"object": "response",
					"output": []
				}
			}
		]
	}`))
	require.NoError(t, err)

	server := httptest.NewServer(handler)
	defer server.Close()

	body := fmt.Sprintf(`{
		"model": "mock-model-case-name",
		"input": [
			{
				"type": "message",
				"content": [
					{"type": "input_text", "text": "system prompt"}
				]
			},
			{
				"type": "message",
				"content": [
					{"type": "input_text", "text": "environment block"}
				]
			},
			{
				"type": "message",
				"content": [
					{"type": "input_text", "text": "actual read %s/catalog/query.go"}
				]
			}
		],
		"tools": [
			{"name": "read_file"}
		],
		"stream": true
	}`, workDir)

	resp, err := http.Post(server.URL+"/responses", "application/json", bytes.NewBufferString(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	fixture := httpFixtureConfig{
		Responses: []httpFixtureResponse{
			{
				Name:    "turn-01",
				Consume: true,
				Request: map[string]any{
					"model": "mock-model-case-name",
					"input": []any{
						map[string]any{
							"type": "message",
							"content": []any{
								map[string]any{"type": "input_text"},
							},
						},
						map[string]any{
							"type": "message",
							"content": []any{
								map[string]any{"type": "input_text"},
							},
						},
						map[string]any{
							"type": "message",
							"content": []any{
								map[string]any{"type": "input_text", "text": "expected user message"},
							},
						},
					},
				},
			},
		},
	}

	augmented := augmentReplayMockOpenAIError(errors.New("run failed"), handler, fixture, []string{workDir})
	message := augmented.Error()

	assert.Contains(t, message, "pruned request sent to mockopenai")
	assert.Contains(t, message, `"actual read __REPO_ROOT__/catalog/query.go"`)
	assert.NotContains(t, message, `"system prompt"`)
	assert.NotContains(t, message, `"environment block"`)
	assert.NotContains(t, message, `"tools"`)
	assert.Contains(t, message, "next non-consumed request in http.json (turn-01)")
	assert.Contains(t, message, `"expected user message"`)
}
