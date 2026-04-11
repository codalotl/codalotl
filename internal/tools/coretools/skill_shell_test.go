package coretools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillShell_BasicallyWorks(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewSkillShellTool(auth)
	call := llmstream.ToolCall{CallID: "call1", Name: ToolNameSkillShell, Type: "function_call", Input: `{"command":["go","version"],"skill":"go-testing"}`}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)

	var payload struct {
		Success bool   `json:"success"`
		Content string `json:"content"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Result), &payload))
	assert.True(t, payload.Success)
	assert.Contains(t, payload.Content, "Command: go version\n")
	assert.Contains(t, payload.Content, "Timeout: false\n")
	assert.Contains(t, payload.Content, "\nOutput:\n")
	assert.Contains(t, strings.ToLower(payload.Content), "go version")
}

func TestSkillShell_Presenter_CommandSummary(t *testing.T) {
	tool := NewSkillShellTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
	presenter := tool.Presenter()
	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  ToolNameSkillShell,
		Input: `{"command":["go","test","./..."],"skill":"git-commit"}`,
	}

	assert.Equal(t, llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Running", Role: llmstream.RoleAction},
				{Text: "go test ./...", Role: llmstream.RoleNormal},
			},
		},
	}, presenter.Present(call, nil))

	assert.Equal(t, llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Ran", Role: llmstream.RoleAction},
				{Text: "go test ./...", Role: llmstream.RoleNormal},
			},
		},
	}, presenter.Present(call, &llmstream.ToolResult{}))
}

func TestSkillShell_Presenter_FallbacksToToolName(t *testing.T) {
	tool := NewSkillShellTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
	presenter := tool.Presenter()
	require.NotNil(t, presenter)

	tests := []struct {
		name     string
		call     llmstream.ToolCall
		expected string
	}{
		{
			name: "invalid json",
			call: llmstream.ToolCall{
				Name:  ToolNameSkillShell,
				Input: `{`,
			},
			expected: ToolNameSkillShell,
		},
		{
			name: "empty command and empty call name",
			call: llmstream.ToolCall{
				Input: `{"command":[],"skill":"git-commit"}`,
			},
			expected: ToolNameShell,
		},
		{
			name: "blank executable",
			call: llmstream.ToolCall{
				Name:  ToolNameSkillShell,
				Input: `{"command":["","test"],"skill":"git-commit"}`,
			},
			expected: ToolNameSkillShell,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			presentation := presenter.Present(tc.call, nil)
			assert.Equal(t, llmstream.Presentation{
				Behavior: llmstream.CompletionBehaviorReplace,
				Summary: llmstream.Line{
					JoinWithSpace: true,
					Segments: []llmstream.Segment{
						{Text: "Running", Role: llmstream.RoleAction},
						{Text: tc.expected, Role: llmstream.RoleNormal},
					},
				},
			}, presentation)
		})
	}
}
