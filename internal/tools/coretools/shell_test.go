package coretools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmstream"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShell_Run_Success(t *testing.T) {
	tool := NewShellTool(t.TempDir(), nil)
	call := llmstream.ToolCall{CallID: "call1", Name: ToolNameShell, Type: "function_call", Input: `{"command":["go","version"]}`}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)

	var payload struct {
		Success bool   `json:"success"`
		Content string `json:"content"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Result), &payload))
	assert.True(t, payload.Success)

	// Build exact expected content using real go version output and the reported duration line.
	expectedOut, err := exec.Command("go", "version").CombinedOutput()
	require.NoError(t, err)
	if len(expectedOut) == 0 || expectedOut[len(expectedOut)-1] != '\n' {
		expectedOut = append(expectedOut, '\n')
	}
	lines := strings.Split(payload.Content, "\n")
	require.GreaterOrEqual(t, len(lines), 6) // Command, Process State, Timeout, Duration, Output, <command output>, ...
	assert.Equal(t, "Command: go version", lines[0])
	assert.Equal(t, "Process State: exit status 0", lines[1])
	assert.Equal(t, "Timeout: false", lines[2])
	assert.True(t, strings.HasPrefix(lines[3], "Duration: "))
	assert.Equal(t, "Output:", lines[4])
	outputBlock := strings.Join(lines[5:], "\n")
	assert.Equal(t, string(expectedOut), outputBlock)
}

func TestShell_Run_NonZeroExit(t *testing.T) {
	tool := NewShellTool(t.TempDir(), nil)
	// Use a command that reliably exits non-zero across platforms
	var input string
	if runtime.GOOS == "windows" {
		// 'cmd /c exit 1'
		input = `{"command":["cmd","/c","exit","1"]}`
	} else {
		input = `{"command":["false"]}`
	}
	call := llmstream.ToolCall{CallID: "call2", Name: ToolNameShell, Type: "function_call", Input: input}

	res := tool.Run(context.Background(), call)
	assert.True(t, res.IsError)
	assert.NotNil(t, res.SourceErr)

	var payload struct {
		Success bool   `json:"success"`
		Content string `json:"content"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Result), &payload))
	assert.False(t, payload.Success)
	assert.Contains(t, payload.Content, "Process State: exit status 1")
	assert.Contains(t, payload.Content, "Timeout: false")
}

func TestShell_Run_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("timeout test is unix-oriented")
	}
	tool := NewShellTool(t.TempDir(), nil)
	// sleep 1s, but give 10ms timeout
	call := llmstream.ToolCall{CallID: "call3", Name: ToolNameShell, Type: "function_call", Input: `{"command":["sleep","1"],"timeout_ms":10}`}

	res := tool.Run(context.Background(), call)
	assert.True(t, res.IsError)
	assert.NotNil(t, res.SourceErr)

	var payload struct {
		Success bool   `json:"success"`
		Content string `json:"content"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Result), &payload))
	assert.False(t, payload.Success)
	if !strings.Contains(payload.Content, "Process State: signal: killed") && !strings.Contains(payload.Content, "Process State: exit status 0") {
		t.Fatalf("unexpected process state: %s", payload.Content)
	}
	assert.Contains(t, payload.Content, "Timeout: true")
}

func TestShell_Run_Cwd(t *testing.T) {
	sandbox := t.TempDir()
	tool := NewShellTool(sandbox, nil)
	// Use 'pwd' (posix) or 'cd' via cmd on windows
	var input string
	if runtime.GOOS == "windows" {
		// print working directory: 'cd'
		input = `{"command":["cmd","/c","cd"],"cwd":"` + strings.ReplaceAll(sandbox, "\\", "\\\\") + `"}`
	} else {
		input = `{"command":["pwd"],"cwd":"` + strings.ReplaceAll(sandbox, "\\", "\\\\") + `"}`
	}
	call := llmstream.ToolCall{CallID: "call4", Name: ToolNameShell, Type: "function_call", Input: input}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)

	var payload struct {
		Success bool   `json:"success"`
		Content string `json:"content"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Result), &payload))
	assert.True(t, payload.Success)

	// Extract last non-empty line of Output section
	lines := strings.Split(payload.Content, "\n")
	// Find index of "Output:" and take following lines
	idx := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "Output:" {
			idx = i
			break
		}
	}
	require.NotEqual(t, -1, idx)
	out := strings.Join(lines[idx+1:], "\n")
	out = strings.TrimSpace(out)
	// On windows, paths are case-insensitive and may have different separators; normalize for comparison
	if runtime.GOOS == "windows" {
		expected := strings.ToLower(strings.ReplaceAll(sandbox, "\\", "/"))
		got := strings.ToLower(strings.ReplaceAll(out, "\\", "/"))
		assert.Contains(t, got, expected)
	} else {
		abs, _ := filepath.Abs(sandbox)
		assert.Equal(t, abs, out)
	}
}

func TestShell_Run_CwdOutsideSandbox(t *testing.T) {
	sandbox := t.TempDir()
	outside := filepath.Dir(sandbox)
	if outside == sandbox {
		t.Skip("unable to determine directory outside sandbox")
	}
	auth := &stubAuthorizer{}
	auth.shellResp = func(requestPermission bool, _ string, cwd string, command []string) error {
		assert.False(t, requestPermission)
		assert.Equal(t, filepath.Clean(outside), filepath.Clean(cwd))
		return assert.AnError
	}
	tool := NewShellTool(sandbox, auth)

	var input string
	if runtime.GOOS == "windows" {
		input = `{"command":["cmd","/c","cd"],"cwd":"` + strings.ReplaceAll(outside, "\\", "\\\\") + `"}`
	} else {
		input = `{"command":["pwd"],"cwd":"` + strings.ReplaceAll(outside, "\\", "\\\\") + `"}`
	}
	call := llmstream.ToolCall{CallID: "call5", Name: ToolNameShell, Type: "function_call", Input: input}

	res := tool.Run(context.Background(), call)
	assert.True(t, res.IsError)
	assert.Equal(t, assert.AnError, res.SourceErr)
	assert.Contains(t, res.Result, assert.AnError.Error())
}

func TestShell_Run_Authorization(t *testing.T) {
	sandbox := t.TempDir()

	tests := []struct {
		name        string
		allow       bool
		expectError bool
	}{
		{name: "allowed", allow: true},
		{name: "denied", allow: false, expectError: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			auth := &stubAuthorizer{}
			auth.shellResp = func(requestPermission bool, _ string, cwd string, command []string) error {
				assert.True(t, requestPermission)
				assert.Equal(t, filepath.Clean(sandbox), filepath.Clean(cwd))
				require.Equal(t, []string{"go", "version"}, command)
				if tc.allow {
					return nil
				}
				return fmt.Errorf("shell authorization denied")
			}
			tool := NewShellTool(sandbox, auth)
			call := llmstream.ToolCall{
				CallID: "auth",
				Name:   ToolNameShell,
				Type:   "function_call",
				Input:  `{"command":["go","version"],"request_permission":true}`,
			}

			res := tool.Run(context.Background(), call)
			if tc.expectError {
				assert.True(t, res.IsError)
				assert.NotNil(t, res.SourceErr)
				assert.Contains(t, res.Result, "shell authorization denied")
			} else {
				assert.False(t, res.IsError)
				assert.Nil(t, res.SourceErr)
			}

			require.Len(t, auth.shellCalls, 1)
		})
	}
}
