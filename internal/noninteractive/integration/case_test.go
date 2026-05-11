package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/build"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/mockllm/mockopenai"
	"github.com/codalotl/codalotl/internal/noninteractive"
	"github.com/codalotl/codalotl/internal/q/cmdrunner"
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

func TestDenormalizeConfigPromptTextRestoresRuntimePaths(t *testing.T) {
	workDir := filepath.Join(string(filepath.Separator), "tmp", "case-root")

	got := denormalizeConfigPromptText(
		"Inspect @"+httpFixtureRepoRootPlaceholder+"/catalog/query.go and @"+httpFixtureGoRootSrcPlaceholder+"/errors/errors.go",
		[]string{workDir},
	)

	assert.Equal(t,
		"Inspect @"+filepath.Join(workDir, "catalog", "query.go")+" and @"+filepath.Join(build.Default.GOROOT, "src", "errors", "errors.go"),
		got,
	)
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

func TestRunCaseDir_ThreadsLintConfigToNoninteractiveExec(t *testing.T) {
	caseDir := t.TempDir()
	repoDir := filepath.Join(caseDir, "repo")
	require.NoError(t, os.MkdirAll(repoDir, 0o755))

	cfg := testCaseConfig{
		Prompt: "Say hello.",
		Lints: lints.Lints{
			Mode: lints.ConfigModeReplace,
			Steps: []lints.Step{
				{
					ID:         "custom-lint",
					Situations: []lints.Situation{lints.SituationPatch},
					Fix: &cmdrunner.Command{
						Command: "echo",
						Args:    []string{"custom-ran"},
					},
				},
			},
		},
		Expected: []map[string]any{
			{"type": "start", "package_path": ""},
			{"type": "user_message", "text": "Say hello."},
			{"type": "done"},
		},
	}
	configData, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(caseDir, "config.json"), append(configData, '\n'), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(caseDir, "http.json"), []byte("{\"responses\":[]}\n"), 0o644))

	origExec := runNoninteractiveExec
	defer func() {
		runNoninteractiveExec = origExec
	}()

	var capturedPrompt string
	var capturedOpts noninteractive.Options
	runNoninteractiveExec = func(prompt string, opts noninteractive.Options) error {
		capturedPrompt = prompt
		capturedOpts = opts
		_, err := fmt.Fprintln(opts.Out, `{"type":"start","package_path":""}`)
		require.NoError(t, err)
		_, err = fmt.Fprintln(opts.Out, `{"type":"user_message","text":"Say hello."}`)
		require.NoError(t, err)
		_, err = fmt.Fprintln(opts.Out, `{"type":"done"}`)
		return err
	}

	require.NoError(t, RunCaseDir(caseDir))

	assert.Equal(t, "Say hello.", capturedPrompt)
	assert.Equal(t, "", capturedOpts.PackagePath)
	require.Len(t, capturedOpts.LintSteps, 1)
	assert.Equal(t, "custom-lint", capturedOpts.LintSteps[0].ID)
	assert.Equal(t, []lints.Situation{lints.SituationPatch}, capturedOpts.LintSteps[0].Situations)
	require.NotNil(t, capturedOpts.LintSteps[0].Fix)
	assert.Equal(t, "echo", capturedOpts.LintSteps[0].Fix.Command)
	assert.Equal(t, []string{"custom-ran"}, capturedOpts.LintSteps[0].Fix.Args)
}

func TestAssertExpectedRepoFileConfigsMatchesText(t *testing.T) {
	originalRoot := t.TempDir()
	actualRoot := t.TempDir()
	rel := filepath.Join(".codalotl", "cas", "clarify-public-api-1", "aa", "bb")

	actualData := []byte(`{"additional_info":{"unix_timestamp":1770000000},"value":{"entries":[{"origin_package":"example.com/clarifyintegration/pricing","target_package":"example.com/clarifyintegration/catalog","identifier":"New","question":"Exactly how does catalog.New normalize product tags?","answer":"Does not trim whitespace. Sorts the remaining tags lexicographically."}]}}`)
	require.NoError(t, os.MkdirAll(filepath.Join(actualRoot, filepath.Dir(rel)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(actualRoot, rel), actualData, 0o644))

	err := assertExpectedRepoFileConfigs([]expectedRepoFileConfig{
		{
			Path: rel,
			Match: map[string]any{
				"match": "partial",
				"texts": []any{
					"origin_package",
					"example.com/clarifyintegration/pricing",
					"target_package",
					"example.com/clarifyintegration/catalog",
					"identifier",
					"New",
					"Exactly how does catalog.New normalize product tags?",
					"Does not trim whitespace.",
					"Sorts the remaining tags lexicographically.",
				},
			},
		},
	}, originalRoot, actualRoot)

	require.NoError(t, err)
}

func TestInsertImplicitStartSubagentEvents(t *testing.T) {
	events := []map[string]any{
		{
			"type": "tool_call",
			"agent": map[string]any{
				"id":    "agent-root",
				"depth": float64(0),
			},
		},
		{
			"type": "tool_call",
			"agent": map[string]any{
				"id":    "agent-child-1",
				"depth": float64(1),
			},
		},
		{
			"type": "tool_complete",
			"agent": map[string]any{
				"id":    "agent-child-1",
				"depth": float64(1),
			},
		},
		{
			"type": "assistant_text",
			"agent": map[string]any{
				"id":    "agent-child-2",
				"depth": float64(1),
			},
		},
	}

	got := insertImplicitStartSubagentEvents(events)

	require.Len(t, got, 6)
	assert.Equal(t, map[string]any{"type": "start_subagent"}, got[1])
	assert.Equal(t, events[1], got[2])
	assert.Equal(t, map[string]any{"type": "start_subagent"}, got[4])
	assert.Equal(t, events[3], got[5])
}

func TestInsertImplicitStartSubagentEventsDoesNotDuplicateExplicitEvents(t *testing.T) {
	events := []map[string]any{
		{"type": "start_subagent"},
		{
			"type": "tool_call",
			"agent": map[string]any{
				"id":    "agent-child",
				"depth": float64(1),
			},
		},
	}

	got := insertImplicitStartSubagentEvents(events)

	assert.Equal(t, events, got)
}
