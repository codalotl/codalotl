package integration

import (
	"encoding/json"
	"go/build"
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/q/cmdrunner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildExpectedEventsOmitsUnstableFields(t *testing.T) {
	actualEvents := []map[string]any{
		{
			"type":         "start",
			"cwd":          "/tmp/work",
			"package_path": ".",
			"model_id":     "gpt-5.5-high",
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
			"type": "start_subagent",
			"agent": map[string]any{
				"id":    "agent-child",
				"depth": float64(1),
			},
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

	require.Len(t, got, 4)
	assert.Equal(t, map[string]any{
		"type":         "start",
		"package_path": ".",
	}, got[0])
	assert.Equal(t, map[string]any{
		"type": "start_subagent",
	}, got[1])
	assert.Equal(t, map[string]any{
		"type": "assistant_text",
		"agent": map[string]any{
			"depth": float64(0),
		},
		"content": "hello",
	}, got[2])
	assert.Equal(t, map[string]any{
		"type": "done",
	}, got[3])
}

func TestBuildExpectedEventsNormalizesPathsAndKeepsExactStrings(t *testing.T) {
	repoRoot := filepath.Join(string(os.PathSeparator), "tmp", "case-root")
	actualEvents := []map[string]any{
		{
			"type":   "permission",
			"prompt": "Approve reading " + filepath.Join(repoRoot, "catalog", "query.go") + "?",
		},
		{
			"type": "tool_complete",
			"result": map[string]any{
				"is_error": false,
				"output":   "<file name=\"" + filepath.Join(repoRoot, "catalog", "query.go") + "\">",
			},
		},
	}

	got, err := buildExpectedEvents(actualEvents, false, []string{repoRoot})
	require.NoError(t, err)

	assert.Equal(t, []map[string]any{
		{
			"type":   "permission",
			"prompt": "Approve reading catalog/query.go?",
		},
		{
			"type": "tool_complete",
			"result": map[string]any{
				"is_error": false,
				"output":   "<file name=\"catalog/query.go\">",
			},
		},
	}, got)
}

func TestBuildGeneratedCaseNormalizesPromptAbsolutePaths(t *testing.T) {
	repoDir := t.TempDir()
	workDir := t.TempDir()

	stdlibPath := filepath.Join(build.Default.GOROOT, "src", "errors", "errors.go")
	opts := CreateOptions{
		Prompt:      "Read @" + filepath.Join(workDir, "note.txt") + " and @" + stdlibPath,
		OutputDir:   filepath.Join(t.TempDir(), "generated-case"),
		PackagePath: "catalog",
	}

	turns := []recordedTurn{
		{
			Request: map[string]any{
				"model": "gpt-5.5-high",
				"input": []any{
					map[string]any{
						"type": "message",
						"content": []any{
							map[string]any{"type": "input_text", "text": "system"},
						},
					},
					map[string]any{
						"type": "message",
						"content": []any{
							map[string]any{"type": "input_text", "text": "env"},
						},
					},
					map[string]any{
						"type": "message",
						"content": []any{
							map[string]any{"type": "input_text", "text": opts.Prompt},
						},
					},
				},
			},
			Response: map[string]any{
				"id":     "resp_1",
				"object": "response",
				"output": []any{},
			},
		},
	}
	actualEvents := []map[string]any{
		{"type": "start", "package_path": "catalog"},
		{"type": "user_message", "text": opts.Prompt},
		{"type": "done"},
	}

	cfg, _, _, err := buildGeneratedCase("generated-case", repoDir, workDir, actualEvents, turns, opts)
	require.NoError(t, err)

	assert.Equal(t, "Read @"+httpFixtureRepoRootPlaceholder+"/note.txt and @"+httpFixtureGoRootSrcPlaceholder+"/errors/errors.go", cfg.Prompt)
}

func TestBuildHTTPFixturePrunesNestedFirstTurnInputText(t *testing.T) {
	turns := []recordedTurn{
		{
			Request: mustJSONObject(t, `{
				"model": "gpt-5.5-high",
				"input": [
					{
						"type": "message",
						"role": "system",
						"content": [{"type": "input_text", "text": "Root system prompt"}]
					},
					{
						"type": "message",
						"role": "user",
						"content": [{"type": "input_text", "text": "Root user prompt"}]
					}
				]
			}`),
			Response: mustJSONObject(t, `{
				"id": "resp_root_1",
				"object": "response",
				"status": "completed",
				"output": []
			}`),
		},
		{
			Request: mustJSONObject(t, `{
				"model": "gpt-5.5-high",
				"input": [
					{
						"type": "message",
						"role": "system",
						"content": [{"type": "input_text", "text": "Nested system prompt"}]
					},
					{
						"type": "message",
						"role": "user",
						"content": [{"type": "input_text", "text": "Nested user prompt"}]
					},
					{
						"type": "message",
						"role": "user",
						"content": [{"type": "input_text", "text": "Nested follow-up prompt"}]
					}
				]
			}`),
			Response: mustJSONObject(t, `{
				"id": "resp_nested_1",
				"object": "response",
				"status": "completed",
				"output": []
			}`),
		},
	}

	got, err := buildHTTPFixture("nested-first-turn", turns, nil)
	require.NoError(t, err)
	require.Len(t, got.Responses, 2)

	assert.Equal(t, []any{
		map[string]any{
			"type": "message",
			"role": "system",
			"content": []any{
				map[string]any{
					"type": "input_text",
				},
			},
		},
		map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{
					"type": "input_text",
				},
			},
		},
		map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{
					"type": "input_text",
					"text": "Nested follow-up prompt",
				},
			},
		},
	}, got.Responses[1].Request["input"])
}

func TestBuildGeneratedCaseCarriesLintConfig(t *testing.T) {
	repoDir := t.TempDir()
	workDir := t.TempDir()

	opts := CreateOptions{
		Prompt:      "Fix the package.",
		OutputDir:   filepath.Join(t.TempDir(), "generated-case"),
		PackagePath: "catalog",
		ReflowWidth: 88,
		Lints: lints.Lints{
			Mode: lints.ConfigModeReplace,
			Steps: []lints.Step{
				{
					ID:         "custom-echo",
					Situations: []lints.Situation{lints.SituationPatch},
					Fix: &cmdrunner.Command{
						Command: "echo",
						Args:    []string{"custom patch lint"},
					},
				},
			},
		},
	}

	turns := []recordedTurn{
		{
			Request: map[string]any{
				"model": "gpt-5.4-high",
				"input": []any{
					map[string]any{
						"type": "message",
						"content": []any{
							map[string]any{"type": "input_text", "text": "system"},
						},
					},
					map[string]any{
						"type": "message",
						"content": []any{
							map[string]any{"type": "input_text", "text": "env"},
						},
					},
					map[string]any{
						"type": "message",
						"content": []any{
							map[string]any{"type": "input_text", "text": opts.Prompt},
						},
					},
				},
			},
			Response: map[string]any{
				"id":     "resp_1",
				"object": "response",
				"output": []any{},
			},
		},
	}
	actualEvents := []map[string]any{
		{"type": "start", "package_path": "catalog"},
		{"type": "user_message", "text": opts.Prompt},
		{"type": "done"},
	}

	cfg, _, _, err := buildGeneratedCase("generated-case", repoDir, workDir, actualEvents, turns, opts)
	require.NoError(t, err)

	assert.Equal(t, 88, cfg.ReflowWidth)
	assert.Equal(t, opts.Lints, cfg.Lints)
}

func TestBuildHTTPFixtureRequestPreservesStructuredRequestAndNormalizesPaths(t *testing.T) {
	repoRoot := filepath.Join(string(os.PathSeparator), "tmp", "case-root")
	turn := recordedTurn{
		Request: map[string]any{
			"model":               "gpt-5.5-high",
			"temperature":         float64(0),
			"prompt_cache_key":    "cache-key",
			"reasoning":           map[string]any{"effort": "medium"},
			"parallel_tool_calls": true,
			"store":               true,
			"stream":              true,
			"context_management":  map[string]any{"type": "auto"},
			"input": []any{
				map[string]any{
					"type": "message",
					"role": "system",
					"content": []any{
						map[string]any{
							"type": "input_text",
							"text": "system prompt",
						},
					},
				},
				map[string]any{
					"type": "message",
					"role": "system",
					"content": []any{
						map[string]any{
							"type": "input_text",
							"text": "environment block",
						},
					},
				},
				map[string]any{
					"type": "message",
					"role": "user",
					"content": []any{
						map[string]any{
							"type": "input_text",
							"text": "cwd: " + repoRoot + "\nread " + filepath.Join(repoRoot, "catalog", "query.go"),
						},
					},
				},
			},
			"tools": []any{
				map[string]any{
					"type":        "function",
					"name":        "read_file",
					"description": "Read files exactly.",
				},
			},
		},
	}

	got, err := buildHTTPFixtureRequest("case-name", true, turn, []string{repoRoot})
	require.NoError(t, err)

	assert.Equal(t, "mock-model-case-name", got["model"])
	assert.Equal(t, float64(0), got["temperature"])
	assert.Equal(t, []any{
		map[string]any{
			"type": "message",
			"role": "system",
			"content": []any{
				map[string]any{
					"type": "input_text",
				},
			},
		},
		map[string]any{
			"type": "message",
			"role": "system",
			"content": []any{
				map[string]any{
					"type": "input_text",
				},
			},
		},
		map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{
					"type": "input_text",
					"text": "cwd: " + httpFixtureRepoRootPlaceholder + "\nread " + httpFixtureRepoRootPlaceholder + "/catalog/query.go",
				},
			},
		},
	}, got["input"])
	_, ok := got["tools"]
	assert.False(t, ok)
	_, ok = got["prompt_cache_key"]
	assert.False(t, ok)
	_, ok = got["reasoning"]
	assert.False(t, ok)
	_, ok = got["parallel_tool_calls"]
	assert.False(t, ok)
	_, ok = got["store"]
	assert.False(t, ok)
	_, ok = got["stream"]
	assert.False(t, ok)
	_, ok = got["context_management"]
	assert.False(t, ok)
}

func TestBuildHTTPFixtureRequestOmitTextKeysFromFirstTwoMessagesOnly(t *testing.T) {
	turn := recordedTurn{
		Request: map[string]any{
			"model": "gpt-5.5-high",
			"input": []any{
				map[string]any{
					"type": "message",
					"content": []any{
						map[string]any{
							"type": "input_text",
							"text": "first",
						},
					},
				},
				map[string]any{
					"type": "message",
					"content": []any{
						map[string]any{
							"type": "input_text",
							"text": "second",
						},
					},
				},
				map[string]any{
					"type": "message",
					"content": []any{
						map[string]any{
							"type": "input_text",
							"text": "third",
						},
					},
				},
			},
		},
	}

	got, err := buildHTTPFixtureRequest("case-name", true, turn, nil)
	require.NoError(t, err)

	assert.Equal(t, []any{
		map[string]any{
			"type": "message",
			"content": []any{
				map[string]any{
					"type": "input_text",
				},
			},
		},
		map[string]any{
			"type": "message",
			"content": []any{
				map[string]any{
					"type": "input_text",
				},
			},
		},
		map[string]any{
			"type": "message",
			"content": []any{
				map[string]any{
					"type": "input_text",
					"text": "third",
				},
			},
		},
	}, got["input"])
}

func TestBuildHTTPFixtureRequestDoesNotPruneLaterTurnInput(t *testing.T) {
	turn := recordedTurn{
		Request: map[string]any{
			"model": "gpt-5.5-high",
			"input": []any{
				map[string]any{"type": "message", "role": "system"},
				map[string]any{"type": "message", "role": "system"},
				map[string]any{"type": "message", "role": "user"},
			},
		},
	}

	got, err := buildHTTPFixtureRequest("case-name", false, turn, nil)
	require.NoError(t, err)
	assert.Len(t, got["input"], 3)
}

func TestBuildReplayDebugHTTPFixtureRequestPrunesFirstTurn(t *testing.T) {
	repoRoot := filepath.Join(string(os.PathSeparator), "tmp", "case-root")
	request := map[string]any{
		"model": "mock-model-case-name",
		"input": []any{
			map[string]any{
				"type": "message",
				"content": []any{
					map[string]any{
						"type": "input_text",
						"text": "system prompt",
					},
				},
			},
			map[string]any{
				"type": "message",
				"content": []any{
					map[string]any{
						"type": "input_text",
						"text": "env block",
					},
				},
			},
			map[string]any{
				"type": "message",
				"content": []any{
					map[string]any{
						"type": "input_text",
						"text": "read " + filepath.Join(repoRoot, "catalog", "query.go"),
					},
				},
			},
		},
		"tools": []any{
			map[string]any{"name": "read_file"},
		},
		"stream": true,
	}

	got, err := buildReplayDebugHTTPFixtureRequest(request, []string{repoRoot})
	require.NoError(t, err)

	assert.Equal(t, []any{
		map[string]any{
			"type": "message",
			"content": []any{
				map[string]any{
					"type": "input_text",
				},
			},
		},
		map[string]any{
			"type": "message",
			"content": []any{
				map[string]any{
					"type": "input_text",
				},
			},
		},
		map[string]any{
			"type": "message",
			"content": []any{
				map[string]any{
					"type": "input_text",
					"text": "read " + httpFixtureRepoRootPlaceholder + "/catalog/query.go",
				},
			},
		},
	}, got["input"])
	_, ok := got["tools"]
	assert.False(t, ok)
	_, ok = got["stream"]
	assert.False(t, ok)
}

func TestBuildHTTPFixtureResponsePreservesRecordedShape(t *testing.T) {
	repoRoot := filepath.Join(string(os.PathSeparator), "tmp", "case-root")
	response := map[string]any{
		"id":     "resp_real_1",
		"object": "response",
		"status": "completed",
		"output": []any{
			map[string]any{
				"id":      "fc_real_1",
				"type":    "function_call",
				"call_id": "call_123",
				"name":    "read_file",
				"arguments": map[string]any{
					"OfResponseToolSearchCallArguments": `{"path":"` + filepath.Join(repoRoot, "catalog", "query.go") + `"}`,
					"OfString":                          `{"path":"` + filepath.Join(repoRoot, "catalog", "query.go") + `"}`,
				},
			},
			map[string]any{
				"id":   "msg_real_2",
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type": "output_text",
						"text": "updated " + filepath.Join(repoRoot, "catalog", "query.go"),
					},
				},
			},
		},
	}

	got, err := buildHTTPFixtureResponse(response, []string{repoRoot})
	require.NoError(t, err)

	assert.Equal(t, map[string]any{
		"id":     "resp_real_1",
		"object": "response",
		"status": "completed",
		"output": []any{
			map[string]any{
				"id":        "fc_real_1",
				"type":      "function_call",
				"call_id":   "call_123",
				"name":      "read_file",
				"arguments": `{"path":"` + httpFixtureRepoRootPlaceholder + `/catalog/query.go"}`,
			},
			map[string]any{
				"id":   "msg_real_2",
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type": "output_text",
						"text": "updated " + httpFixtureRepoRootPlaceholder + "/catalog/query.go",
					},
				},
			},
		},
	}, got)
}

func TestNormalizeAbsolutePathTextMakesRepoPathsRelative(t *testing.T) {
	repoRoot := filepath.Join(string(os.PathSeparator), "tmp", "case-root")
	input := "read " + filepath.Join(repoRoot, "catalog", "query.go") + ":12"

	assert.Equal(t, "read catalog/query.go:12", normalizeAbsolutePathText(input, []string{repoRoot}))
}

func TestNormalizeAbsolutePathTextMakesSystemSkillPathsPortable(t *testing.T) {
	systemSkillsDir := defaultSystemSkillsPath()
	require.NotEmpty(t, systemSkillsDir)

	input := "read " + filepath.Join(systemSkillsDir, "spec-md", "SKILL.md")

	assert.Equal(t, "read spec-md/SKILL.md", normalizeAbsolutePathText(input, nil))
	assert.Equal(t, "read spec-md/SKILL.md", normalizeAbsolutePathText("read "+httpFixtureSystemSkillsPlaceholder+"/spec-md/SKILL.md", nil))
	assert.Equal(t, "read "+httpFixtureSystemSkillsPlaceholder+"/spec-md/SKILL.md", normalizeHTTPAbsolutePathText(input, nil))
}

func TestLoadHTTPFixtureDataDenormalizesPaths(t *testing.T) {
	repoRoot := filepath.Join(string(os.PathSeparator), "tmp", "case-root")
	systemSkillsDir := defaultSystemSkillsPath()
	require.NotEmpty(t, systemSkillsDir)
	caseDir := t.TempDir()
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
								map[string]any{
									"type": "input_text",
									"text": "cwd: " + httpFixtureRepoRootPlaceholder,
								},
							},
						},
					},
				},
				Response: map[string]any{
					"id": "resp_real_1",
					"output": []any{
						map[string]any{
							"id":        "fc_real_1",
							"type":      "function_call",
							"arguments": `{"path":"` + httpFixtureRepoRootPlaceholder + `/catalog/query.go"}`,
						},
						map[string]any{
							"id":        "fc_real_2",
							"type":      "function_call",
							"arguments": `{"path":"` + httpFixtureSystemSkillsPlaceholder + `/spec-md/SKILL.md"}`,
						},
					},
				},
			},
		},
	}
	require.NoError(t, writeJSONFile(filepath.Join(caseDir, "http.json"), fixture))

	data, err := loadHTTPFixtureData(filepath.Join(caseDir, "http.json"), []string{repoRoot})
	require.NoError(t, err)

	var got httpFixtureConfig
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "cwd: "+repoRoot, got.Responses[0].Request["input"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"])
	assert.Equal(t, `{"path":"`+filepath.Join(repoRoot, "catalog", "query.go")+`"}`, got.Responses[0].Response["output"].([]any)[0].(map[string]any)["arguments"])
	assert.Equal(t, `{"path":"`+filepath.Join(systemSkillsDir, "spec-md", "SKILL.md")+`"}`, got.Responses[0].Response["output"].([]any)[1].(map[string]any)["arguments"])
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
			"model_id":     "gpt-5.5-high",
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
				"output":   "<apply-patch ok=\"true\">\nUpdated the following files:\nM note.txt\n</apply-patch>",
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
				"model": "gpt-5.5-high",
				"input": [
					{
						"type": "message",
						"role": "system",
						"content": [{"type": "input_text", "text": "System prompt"}]
					},
					{
						"type": "message",
						"role": "system",
						"content": [{"type": "input_text", "text": "Environment block"}]
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
				"model": "gpt-5.5-high",
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
	assert.Equal(t, "resp_real_1", httpCfg.Responses[1].Request["previous_response_id"])
	assert.Equal(t, "resp_real_1", httpCfg.Responses[0].Response["id"])
	assert.Equal(t, "resp_real_2", httpCfg.Responses[1].Response["id"])
	_, ok := httpCfg.Responses[0].Request["tools"]
	assert.False(t, ok)
	assert.Equal(t, []any{
		map[string]any{
			"type": "message",
			"role": "system",
			"content": []any{
				map[string]any{
					"type": "input_text",
				},
			},
		},
		map[string]any{
			"type": "message",
			"role": "system",
			"content": []any{
				map[string]any{
					"type": "input_text",
				},
			},
		},
		map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{
					"type": "input_text",
					"text": "Update @note.txt so it says \"After mutation.\" instead of \"Before mutation.\".",
				},
			},
		},
	}, httpCfg.Responses[0].Request["input"])
	assert.Equal(t, []any{
		map[string]any{
			"id":      "ct_real_1",
			"type":    "custom_tool_call",
			"call_id": "call_apply_patch_note",
			"name":    "apply_patch",
			"input":   "*** Begin Patch\n*** Update File: note.txt\n@@\n-Before mutation.\n+After mutation.\n*** End Patch\n",
		},
	}, httpCfg.Responses[0].Response["output"])
}

func TestMarshalConfigJSONUsesSingleLineExpectedItems(t *testing.T) {
	cfg := testCaseConfig{
		Prompt:      "fix bug",
		PackagePath: "catalog",
		ReflowWidth: 120,
		Lints: lints.Lints{
			Mode: lints.ConfigModeReplace,
			Steps: []lints.Step{
				{
					ID:         "custom-echo",
					Situations: []lints.Situation{lints.SituationPatch},
					Fix: &cmdrunner.Command{
						Command: "echo",
						Args:    []string{"custom patch lint"},
					},
				},
			},
		},
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
	assert.Contains(t, string(data), "  \"reflowwidth\": 120,\n")
	assert.Contains(t, string(data), "  \"lints\": {\n")
	assert.Contains(t, string(data), "    \"mode\": \"replace\",\n")
	assert.Contains(t, string(data), "    \"steps\": [\n")
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
