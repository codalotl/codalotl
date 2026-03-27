package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildExpectedEventsOmitsUnstableFields(t *testing.T) {
	actualEvents := []map[string]any{
		{
			"type":         "start",
			"cwd":          "/tmp/work",
			"package_path": ".",
			"model_id":     "gpt-5.4-high",
		},
		{
			"type": "assistant_reasoning",
			"agent": map[string]any{
				"id":    "agent-root",
				"depth": float64(0),
			},
			"content": "thinking",
		},
		{
			"type": "assistant_text",
			"agent": map[string]any{
				"id":    "agent-root",
				"depth": float64(0),
			},
			"content": "hello",
		},
		{
			"type": "done",
			"token_usage": map[string]any{
				"input": float64(12),
			},
		},
	}

	got, err := buildExpectedEvents(actualEvents, false, nil)
	require.NoError(t, err)

	require.Len(t, got, 3)
	assert.Equal(t, map[string]any{
		"type":         "start",
		"package_path": ".",
	}, got[0])
	assert.Equal(t, map[string]any{
		"type": "assistant_text",
		"agent": map[string]any{
			"depth": float64(0),
		},
		"content": "hello",
	}, got[1])
	assert.Equal(t, map[string]any{
		"type": "done",
	}, got[2])
}

func TestChooseRequestSnippetSkipsStructuralLeaves(t *testing.T) {
	input := mustJSONObject(t, `{
		"type": "message",
		"role": "user",
		"content": [
			{"type": "input_text", "text": "add func Product"}
		]
	}`)

	got, ok := chooseRequestSnippet(input, nil)
	require.True(t, ok)
	assert.Equal(t, "add func Product", got)
}

func TestNormalizeResponseOutputItemExtractsMinimalFunctionCallShape(t *testing.T) {
	item := mustJSONObject(t, `{
		"type": "function_call",
		"call_id": "call_123",
		"name": "read_file",
		"arguments": {
			"OfResponseToolSearchCallArguments": "{\"path\":\"mathutil/sum.go\"}",
			"OfString": "{\"path\":\"mathutil/sum.go\"}"
		}
	}`)

	got := normalizeResponseOutputItem("case", 0, 0, item)

	assert.Equal(t, map[string]any{
		"id":        "fc_case_1_1",
		"type":      "function_call",
		"call_id":   "call_123",
		"name":      "read_file",
		"arguments": "{\"path\":\"mathutil/sum.go\"}",
	}, got)
}

func TestStableSnippetUsesStableTagPrefixForMultilineToolOutput(t *testing.T) {
	got := stableSnippet("<test-status ok=\"true\">\n$ go test ./mathutil\nok  \texample.com/clarifyintegration/mathutil\t0.166s\n</test-status>", nil)
	assert.Equal(t, "<test-status ok=\"true\">", got)
}

func TestBuildGeneratedCaseReplaysMutation(t *testing.T) {
	repoDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "note.txt"), []byte("Before mutation.\n"), 0o644))

	workDir := t.TempDir()
	require.NoError(t, copyTree(repoDir, workDir))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "note.txt"), []byte("After mutation.\n"), 0o644))

	actualEvents := []map[string]any{
		{
			"type":         "start",
			"cwd":          workDir,
			"package_path": "",
			"model_id":     "gpt-5.4-high",
		},
		{
			"type": "user_message",
			"text": "Update @note.txt so it says \"After mutation.\" instead of \"Before mutation.\".",
		},
		{
			"type": "tool_call",
			"agent": map[string]any{
				"id":    "agent-root",
				"depth": float64(0),
			},
			"tool": map[string]any{
				"call_id": "call_apply_patch_note",
				"name":    "apply_patch",
				"type":    "custom_tool_call",
				"input":   "*** Begin Patch\n*** Update File: note.txt\n@@\n-Before mutation.\n+After mutation.\n*** End Patch\n",
			},
		},
		{
			"type": "tool_complete",
			"agent": map[string]any{
				"id":    "agent-root",
				"depth": float64(0),
			},
			"tool": map[string]any{
				"call_id": "call_apply_patch_note",
				"name":    "apply_patch",
				"type":    "custom_tool_call",
			},
			"result": map[string]any{
				"is_error": false,
				"output":   "<apply-patch ok=\"true\">",
			},
		},
		{
			"type": "assistant_text",
			"agent": map[string]any{
				"id":    "agent-root",
				"depth": float64(0),
			},
			"content": "Updated note.txt to say After mutation.",
		},
		{
			"type": "done",
			"token_usage": map[string]any{
				"input":  float64(30),
				"output": float64(11),
				"total":  float64(41),
			},
		},
	}

	turns := []recordedTurn{
		{
			Request: mustJSONObject(t, `{
				"model": "gpt-5.4-high",
				"input": [
					{
						"type": "message",
						"role": "system",
						"content": [{"type": "input_text", "text": "System prompt"}]
					},
					{
						"type": "message",
						"role": "user",
						"content": [{"type": "input_text", "text": "Update @note.txt so it says \"After mutation.\" instead of \"Before mutation.\"."}]
					}
				],
				"tools": [{"type": "custom", "name": "apply_patch"}]
			}`),
			Response: mustJSONObject(t, `{
				"id": "resp_real_1",
				"object": "response",
				"status": "completed",
				"usage": {"input_tokens": 18, "output_tokens": 4},
				"output": [
					{
						"id": "ct_real_1",
						"type": "custom_tool_call",
						"call_id": "call_apply_patch_note",
						"name": "apply_patch",
						"input": "*** Begin Patch\n*** Update File: note.txt\n@@\n-Before mutation.\n+After mutation.\n*** End Patch\n"
					}
				]
			}`),
		},
		{
			Request: mustJSONObject(t, `{
				"model": "gpt-5.4-high",
				"previous_response_id": "resp_real_1",
				"input": [
					{
						"type": "custom_tool_call_output",
						"call_id": "call_apply_patch_note",
						"output": "<apply-patch ok=\"true\">"
					}
				]
			}`),
			Response: mustJSONObject(t, `{
				"id": "resp_real_2",
				"object": "response",
				"status": "completed",
				"usage": {"input_tokens": 12, "output_tokens": 7},
				"output": [
					{
						"id": "msg_real_2",
						"type": "message",
						"role": "assistant",
						"content": [
							{"type": "output_text", "text": "Updated note.txt to say After mutation."}
						]
					}
				]
			}`),
		},
	}

	opts := CreateOptions{
		Prompt:            "Update @note.txt so it says \"After mutation.\" instead of \"Before mutation.\".",
		OutputDir:         filepath.Join(t.TempDir(), "generated-basic-mutation"),
		IncludeTokenUsage: false,
	}

	cfg, httpCfg, expectedRepoFiles, err := buildGeneratedCase("generated-basic-mutation", repoDir, workDir, actualEvents, turns, opts)
	require.NoError(t, err)

	assert.Equal(t, "Update @note.txt so it says \"After mutation.\" instead of \"Before mutation.\".", cfg.Prompt)
	assert.Equal(t, map[string]string{
		"note.txt": "After mutation.\n",
	}, expectedRepoFiles)
	require.Len(t, httpCfg.Responses, 2)
	assert.Equal(t, "mock-model-generated-basic-mutation", httpCfg.Responses[0].Request["model"])
	assert.Equal(t, "resp_generated-basic-mutation_1", httpCfg.Responses[1].Request["previous_response_id"])

	caseDir := filepath.Join(t.TempDir(), "generated-basic-mutation")
	require.NoError(t, os.MkdirAll(caseDir, 0o755))
	require.NoError(t, writeConfigJSONFile(filepath.Join(caseDir, "config.json"), cfg))
	require.NoError(t, writeJSONFile(filepath.Join(caseDir, "http.json"), httpCfg))
	require.NoError(t, copyTree(repoDir, filepath.Join(caseDir, "repo")))
	require.NoError(t, writeExpectedRepoFiles(filepath.Join(caseDir, "expected_repo"), expectedRepoFiles))

	require.NoError(t, RunCaseDir(caseDir))
}

func TestMarshalConfigJSONUsesSingleLineExpectedItems(t *testing.T) {
	cfg := testCaseConfig{
		Prompt:      "fix bug",
		PackagePath: "catalog",
		Expected: []map[string]any{
			{
				"type":         "start",
				"package_path": "catalog",
			},
			{
				"type": "tool_call",
				"tool": map[string]any{
					"name":  "read_file",
					"input": "{\"path\":\"catalog/query.go\"}",
				},
			},
		},
	}

	data, err := marshalConfigJSON(cfg)
	require.NoError(t, err)

	assert.Contains(t, string(data), "  \"expected\": [\n")
	assert.Contains(t, string(data), "    {\"package_path\":\"catalog\",\"type\":\"start\"},\n")
	assert.Contains(t, string(data), "    {\"tool\":{\"input\":\"{\\\"path\\\":\\\"catalog/query.go\\\"}\",\"name\":\"read_file\"},\"type\":\"tool_call\"}\n")

	var roundTrip testCaseConfig
	require.NoError(t, json.Unmarshal(data, &roundTrip))
	assert.Equal(t, cfg, roundTrip)
}

func mustJSONObject(t *testing.T, raw string) map[string]any {
	t.Helper()

	var value map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &value))
	return value
}
