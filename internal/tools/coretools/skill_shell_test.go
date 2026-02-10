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
