package noninteractive

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/stretchr/testify/require"
)

func TestBuildJSONTokenUsage(t *testing.T) {
	t.Parallel()

	got := buildJSONTokenUsage(llmstream.TokenUsage{
		TotalInputTokens:         100,
		CachedInputTokens:        40,
		CacheCreationInputTokens: 9,
		TotalOutputTokens:        7,
	})

	require.Equal(t, jsonTokenUsage{
		Input:       60,
		CachedInput: 40,
		CacheWrites: 9,
		Output:      7,
		Total:       107,
	}, got)
}

func TestJSONEventWriterWriteAgentEvent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		event   agent.Event
		want    map[string]any
		wantOut bool
	}{
		{
			name: "assistant text",
			event: agent.Event{
				Type:        agent.EventTypeAssistantText,
				Agent:       agent.AgentMeta{ID: "root", Depth: 0},
				TextContent: llmstream.TextContent{Content: "hello"},
			},
			want: map[string]any{
				"type":    "assistant_text",
				"content": "hello",
				"agent": map[string]any{
					"id":    "root",
					"depth": float64(0),
				},
			},
			wantOut: true,
		},
		{
			name: "assistant reasoning",
			event: agent.Event{
				Type:             agent.EventTypeAssistantReasoning,
				Agent:            agent.AgentMeta{ID: "sub", Depth: 1},
				ReasoningContent: llmstream.ReasoningContent{Content: "thinking"},
			},
			want: map[string]any{
				"type":    "assistant_reasoning",
				"content": "thinking",
				"agent": map[string]any{
					"id":    "sub",
					"depth": float64(1),
				},
			},
			wantOut: true,
		},
		{
			name: "tool call",
			event: agent.Event{
				Type:  agent.EventTypeToolCall,
				Agent: agent.AgentMeta{ID: "root", Depth: 0},
				Tool:  namedTestTool{name: "read_file"},
				ToolCall: &llmstream.ToolCall{
					CallID: "call_1",
					Name:   "ignored_call_name",
					Type:   "function_call",
					Input:  `{"path":"foo.go"}`,
				},
			},
			want: map[string]any{
				"type": "tool_call",
				"agent": map[string]any{
					"id":    "root",
					"depth": float64(0),
				},
				"tool": map[string]any{
					"call_id": "call_1",
					"name":    "read_file",
					"type":    "function_call",
					"input":   `{"path":"foo.go"}`,
				},
			},
			wantOut: true,
		},
		{
			name: "tool complete",
			event: agent.Event{
				Type:  agent.EventTypeToolComplete,
				Agent: agent.AgentMeta{ID: "root", Depth: 0},
				Tool:  namedTestTool{name: "read_file"},
				ToolResult: &llmstream.ToolResult{
					CallID:  "call_1",
					Name:    "ignored_result_name",
					Type:    "function_call",
					Result:  "package foo\n",
					IsError: false,
				},
			},
			want: map[string]any{
				"type": "tool_complete",
				"agent": map[string]any{
					"id":    "root",
					"depth": float64(0),
				},
				"tool": map[string]any{
					"call_id": "call_1",
					"name":    "read_file",
					"type":    "function_call",
				},
				"result": map[string]any{
					"output":   "package foo\n",
					"is_error": false,
				},
			},
			wantOut: true,
		},
		{
			name: "tool call falls back to tool result name when tool call missing",
			event: agent.Event{
				Type:  agent.EventTypeToolCall,
				Agent: agent.AgentMeta{ID: "root", Depth: 0},
				ToolResult: &llmstream.ToolResult{
					CallID: "call_2",
					Name:   "read_file",
					Type:   "function_call",
				},
			},
			want: map[string]any{
				"type": "tool_call",
				"agent": map[string]any{
					"id":    "root",
					"depth": float64(0),
				},
				"tool": map[string]any{
					"call_id": "call_2",
					"name":    "read_file",
					"type":    "function_call",
				},
			},
			wantOut: true,
		},
		{
			name: "tool complete falls back to tool call name when tool result missing",
			event: agent.Event{
				Type:  agent.EventTypeToolComplete,
				Agent: agent.AgentMeta{ID: "root", Depth: 0},
				ToolCall: &llmstream.ToolCall{
					CallID: "call_2",
					Name:   "read_file",
					Type:   "function_call",
				},
			},
			want: map[string]any{
				"type": "tool_complete",
				"agent": map[string]any{
					"id":    "root",
					"depth": float64(0),
				},
				"tool": map[string]any{
					"call_id": "call_2",
					"name":    "read_file",
					"type":    "function_call",
				},
				"result": map[string]any{
					"output":   "",
					"is_error": false,
				},
			},
			wantOut: true,
		},
		{
			name: "warning",
			event: agent.Event{
				Type:  agent.EventTypeWarning,
				Agent: agent.AgentMeta{ID: "root", Depth: 0},
				Error: errors.New("watch out"),
			},
			want: map[string]any{
				"type":    "warning",
				"message": "watch out",
				"agent": map[string]any{
					"id":    "root",
					"depth": float64(0),
				},
			},
			wantOut: true,
		},
		{
			name: "start subagent is ignored",
			event: agent.Event{
				Type:  agent.EventTypeStartSubagent,
				Agent: agent.AgentMeta{ID: "sub", Depth: 1, Parent: "root"},
				StartSubagent: agent.StartSubagent{
					CallingAgentID: "root",
					ToolCallID:     "call_1",
					Label:          "clarify_public_api",
				},
			},
			wantOut: false,
		},
		{
			name: "ignored event",
			event: agent.Event{
				Type: agent.EventTypeQueuedUserMessageSent,
			},
			wantOut: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			w := newJSONEventWriter(&buf)

			require.NoError(t, w.WriteAgentEvent(tc.event))

			if !tc.wantOut {
				require.Empty(t, buf.String())
				return
			}

			var got map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
			require.Equal(t, tc.want, got)
		})
	}
}

func TestJSONEventWriterWriteDone(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := newJSONEventWriter(&buf)

	actual := llmstream.TokenUsage{
		TotalInputTokens:         100,
		CachedInputTokens:        40,
		CacheCreationInputTokens: 9,
		TotalOutputTokens:        7,
	}
	ideal := llmstream.TokenUsage{
		TotalInputTokens:  30,
		CachedInputTokens: 20,
		TotalOutputTokens: 5,
	}

	require.NoError(t, w.WriteDone(actual, &ideal))

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Equal(t, "done", got["type"])
	require.Equal(t, map[string]any{
		"input":        float64(60),
		"cached_input": float64(40),
		"cache_writes": float64(9),
		"output":       float64(7),
		"total":        float64(107),
	}, got["token_usage"])
	require.Equal(t, map[string]any{
		"input":        float64(10),
		"cached_input": float64(20),
		"cache_writes": float64(0),
		"output":       float64(5),
		"total":        float64(35),
	}, got["ideal_token_usage"])
}

func TestJSONEventWriterWriteStartAndUserMessage(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := newJSONEventWriter(&buf)

	require.NoError(t, w.WriteStart("/tmp/sandbox", "internal/noninteractive", llmmodel.ModelID("gpt-5.4-high")))
	require.NoError(t, w.WriteUserMessage("fix failing test"))

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte{'\n'})
	require.Len(t, lines, 2)

	var start map[string]any
	require.NoError(t, json.Unmarshal(lines[0], &start))
	require.Equal(t, map[string]any{
		"type":         "start",
		"cwd":          "/tmp/sandbox",
		"package_path": "internal/noninteractive",
		"model_id":     "gpt-5.4-high",
	}, start)

	var msg map[string]any
	require.NoError(t, json.Unmarshal(lines[1], &msg))
	require.Equal(t, map[string]any{
		"type": "user_message",
		"text": "fix failing test",
	}, msg)
}

func TestWriteSessionStartOutputJSON_OnlyEmitsEndUserPrompt(t *testing.T) {
	t.Parallel()

	t.Run("orchestrate without initial prompt omits user_message", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		require.NoError(t, writeStepStartOutput(&buf, newJSONEventWriter(&buf), true, stepStartOutput{
			sandboxDir: "/tmp/sandbox",
			modelID:    llmmodel.ModelID("gpt-5.4-high"),
		}, ""))

		lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte{'\n'})
		require.Len(t, lines, 1)

		var event map[string]any
		require.NoError(t, json.Unmarshal(lines[0], &event))
		require.Equal(t, map[string]any{
			"type":         "start",
			"cwd":          "/tmp/sandbox",
			"package_path": "",
			"model_id":     "gpt-5.4-high",
		}, event)
	})

	t.Run("orchestrate with initial prompt emits only that prompt", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		require.NoError(t, writeStepStartOutput(&buf, newJSONEventWriter(&buf), true, stepStartOutput{
			sandboxDir: "/tmp/sandbox",
			modelID:    llmmodel.ModelID("gpt-5.4-high"),
		}, "fix failing test"))

		lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte{'\n'})
		require.Len(t, lines, 2)

		var msg map[string]any
		require.NoError(t, json.Unmarshal(lines[1], &msg))
		require.Equal(t, map[string]any{
			"type": "user_message",
			"text": "fix failing test",
		}, msg)
	})
}

func TestAutoRespondToUserRequestsJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	reqs := make(chan authdomain.UserRequest, 1)
	allowed := false
	reqs <- authdomain.UserRequest{
		Prompt: "Allow read_file?",
		Allow: func() {
			allowed = true
		},
		Disallow: func() {
			t.Fatal("expected allow")
		},
	}
	close(reqs)

	autoRespondToUserRequests(reqs, &buf, true, newJSONEventWriter(&buf), true)

	require.True(t, allowed)

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Equal(t, map[string]any{
		"type":      "permission",
		"prompt":    "Allow read_file?",
		"decision":  "allow",
		"automatic": true,
	}, got)
}
