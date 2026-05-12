package coretools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellOutputHelper(t *testing.T) {
	mode := os.Getenv("CORETOOLS_SHELL_OUTPUT_HELPER")
	if mode == "" {
		return
	}

	switch mode {
	case "large":
		fmt.Print("HEAD-")
		fmt.Print(strings.Repeat("m", defaultShellMaxOutputBytes+10_000))
		fmt.Print("-TAIL")
	case "small":
		fmt.Print(strings.Repeat("s", 5_000))
	default:
		fmt.Print(mode)
	}
	os.Exit(0)
}

type shellTestResultPayload struct {
	Success bool   `json:"success"`
	Content string `json:"content"`
}

func shellTestInput(t *testing.T, command []string, maxOutputBytes int) string {
	t.Helper()
	params := map[string]any{
		"command": command,
	}
	if maxOutputBytes > 0 {
		params["max_output_bytes"] = maxOutputBytes
	}
	b, err := json.Marshal(params)
	require.NoError(t, err)
	return string(b)
}

func shellTestPayload(t *testing.T, res llmstream.ToolResult) shellTestResultPayload {
	t.Helper()
	var payload shellTestResultPayload
	require.NoError(t, json.Unmarshal([]byte(res.Result), &payload))
	return payload
}

func shellTestOutputBlock(t *testing.T, content string) string {
	t.Helper()
	_, output, ok := strings.Cut(content, "Output:\n")
	require.True(t, ok)
	return output
}

func TestShell_Run_Success(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewShellTool(auth)
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
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewShellTool(auth)
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
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewShellTool(auth)
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

func TestShell_Run_DefaultOutputLimit(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewShellTool(auth)
	t.Setenv("CORETOOLS_SHELL_OUTPUT_HELPER", "large")

	call := llmstream.ToolCall{
		CallID: "limit-default",
		Name:   ToolNameShell,
		Type:   "function_call",
		Input:  shellTestInput(t, []string{os.Args[0], "-test.run=TestShellOutputHelper"}, 0),
	}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)

	payload := shellTestPayload(t, res)
	assert.True(t, payload.Success)
	assert.Contains(t, payload.Content, "Command: ")
	assert.Contains(t, payload.Content, "Process State: exit status 0")
	assert.Contains(t, payload.Content, "Timeout: false")
	output := shellTestOutputBlock(t, payload.Content)
	assert.LessOrEqual(t, len([]byte(output)), defaultShellMaxOutputBytes+1)
	assert.Contains(t, output, "HEAD-")
	assert.Contains(t, output, "-TAIL")
	assert.Contains(t, output, "[...")
	assert.Contains(t, output, "bytes elided ...]")
}

func TestShell_Run_CustomOutputLimit(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewShellTool(auth)
	t.Setenv("CORETOOLS_SHELL_OUTPUT_HELPER", "large")

	call := llmstream.ToolCall{
		CallID: "limit-custom",
		Name:   ToolNameShell,
		Type:   "function_call",
		Input:  shellTestInput(t, []string{os.Args[0], "-test.run=TestShellOutputHelper"}, 2048),
	}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)

	payload := shellTestPayload(t, res)
	assert.True(t, payload.Success)
	output := shellTestOutputBlock(t, payload.Content)
	assert.LessOrEqual(t, len([]byte(output)), 2049)
	assert.Contains(t, output, "HEAD-")
	assert.Contains(t, output, "-TAIL")
	assert.Contains(t, output, "bytes elided")
}

func TestShell_Info_MaxOutputBytesOmitsUnsupportedNumericSchemaKeywords(t *testing.T) {
	tool := NewShellTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))

	paramSchema, ok := tool.Info().Parameters["max_output_bytes"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "integer", paramSchema["type"])
	assert.NotContains(t, paramSchema, "default")
	assert.NotContains(t, paramSchema, "minimum")
	assert.NotContains(t, paramSchema, "maximum")
	assert.Contains(t, paramSchema["description"], "default 40000")
	assert.Contains(t, paramSchema["description"], "clamped to 1024..1048576")
}

func TestShell_MaxOutputBytesBounds(t *testing.T) {
	assert.Equal(t, defaultShellMaxOutputBytes, effectiveShellMaxOutputBytes(0))
	assert.Equal(t, defaultShellMaxOutputBytes, effectiveShellMaxOutputBytes(-1))
	assert.Equal(t, minShellMaxOutputBytes, effectiveShellMaxOutputBytes(1))
	assert.Equal(t, minShellMaxOutputBytes, effectiveShellMaxOutputBytes(minShellMaxOutputBytes-1))
	assert.Equal(t, minShellMaxOutputBytes, effectiveShellMaxOutputBytes(minShellMaxOutputBytes))
	assert.Equal(t, 2048, effectiveShellMaxOutputBytes(2048))
	assert.Equal(t, maxShellMaxOutputBytes, effectiveShellMaxOutputBytes(maxShellMaxOutputBytes+1))
}

func TestShell_LimitOutputBytesPreservesValidUTF8(t *testing.T) {
	output := []byte("HEAD-" + strings.Repeat("🙂", 600) + "-TAIL")

	limited := limitShellOutputBytes(output, minShellMaxOutputBytes)

	require.True(t, utf8.Valid(limited))
	assert.LessOrEqual(t, len(limited), minShellMaxOutputBytes)
	assert.True(t, strings.HasPrefix(string(limited), "HEAD-"))
	assert.True(t, strings.HasSuffix(string(limited), "-TAIL"))
	assert.Contains(t, string(limited), "bytes elided")
}

func TestShell_Run_Cwd(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewShellTool(auth)
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
	auth := &stubAuthorizer{sandboxDir: sandbox}
	auth.shellResp = func(requestPermission bool, _ string, cwd string, command []string) error {
		assert.False(t, requestPermission)
		assert.Equal(t, filepath.Clean(outside), filepath.Clean(cwd))
		return assert.AnError
	}
	tool := NewShellTool(auth)

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
			auth := &stubAuthorizer{sandboxDir: sandbox}
			auth.shellResp = func(requestPermission bool, _ string, cwd string, command []string) error {
				assert.True(t, requestPermission)
				assert.Equal(t, filepath.Clean(sandbox), filepath.Clean(cwd))
				require.Equal(t, []string{"go", "version"}, command)
				if tc.allow {
					return nil
				}
				return fmt.Errorf("shell authorization denied")
			}
			tool := NewShellTool(auth)
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

func TestShell_Presenter_CommandSummary(t *testing.T) {
	tool := NewShellTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
	presenter := tool.Presenter()
	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  ToolNameShell,
		Input: `{"command":["go","test","."]}`,
	}

	assert.Equal(t, llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Running", Role: llmstream.RoleAction},
				{Text: "go test .", Role: llmstream.RoleNormal},
			},
		},
	}, presenter.Present(call, nil))

	assert.Equal(t, llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Ran", Role: llmstream.RoleAction},
				{Text: "go test .", Role: llmstream.RoleNormal},
			},
		},
	}, presenter.Present(call, &llmstream.ToolResult{}))
}

func TestShell_Presenter_CompleteIncludesSummarizedOutput(t *testing.T) {
	tool := NewShellTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
	presenter := tool.Presenter()
	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  ToolNameShell,
		Input: `{"command":["go","test","."]}`,
	}
	result := &llmstream.ToolResult{
		Result: `{"success":true,"content":"Command: go test .\nProcess State: exit status 0\nTimeout: false\nDuration: 10ms\nOutput:\nok   github.com/codalotl/codalotl/internal/tools/coretools\t0.123s\n?    github.com/codalotl/codalotl/internal/tools/coretools/testdata\t[no test files]\n"}`,
	}

	assert.Equal(t, llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Ran", Role: llmstream.RoleAction},
				{Text: "go test .", Role: llmstream.RoleNormal},
			},
		},
		Body: llmstream.Output{
			Lines: []string{
				"ok   github.com/codalotl/codalotl/internal/tools/coretools\t0.123s",
				"?    github.com/codalotl/codalotl/internal/tools/coretools/testdata\t[no test files]",
			},
		},
	}, presenter.Present(call, result))
}

func TestShell_Presenter_CompleteSummarizesLongOutput(t *testing.T) {
	tool := NewShellTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
	presenter := tool.Presenter()
	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  ToolNameShell,
		Input: `{"command":["go","test","./..."]}`,
	}
	result := &llmstream.ToolResult{
		Result: `{"success":true,"content":"Command: go test ./...\nProcess State: exit status 0\nTimeout: false\nDuration: 10ms\nOutput:\nline 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\n"}`,
	}

	assert.Equal(t, llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Ran", Role: llmstream.RoleAction},
				{Text: "go test ./...", Role: llmstream.RoleNormal},
			},
		},
		Body: llmstream.Output{
			Lines: []string{
				"line 1",
				"line 2",
				"line 3",
				"line 4",
				"line 5",
			},
			OmittedLineCount: 2,
		},
	}, presenter.Present(call, result))
}

func TestShell_Presenter_CompleteShowsStructuredError(t *testing.T) {
	tool := NewShellTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
	presenter := tool.Presenter()
	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  ToolNameShell,
		Input: `{"command":["go","test","."]}`,
	}
	result := &llmstream.ToolResult{
		IsError: true,
		Result:  `{"error":"shell authorization denied"}`,
	}

	assert.Equal(t, llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Ran", Role: llmstream.RoleAction},
				{Text: "go test .", Role: llmstream.RoleNormal},
			},
		},
		Body: llmstream.Output{
			Lines: []string{"Error: shell authorization denied"},
		},
	}, presenter.Present(call, result))
}

func TestShell_Presenter_FallbacksToToolName(t *testing.T) {
	tool := NewShellTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()))
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
				Name:  ToolNameShell,
				Input: `{`,
			},
			expected: ToolNameShell,
		},
		{
			name: "empty command and empty call name",
			call: llmstream.ToolCall{
				Input: `{"command":[]}`,
			},
			expected: ToolNameShell,
		},
		{
			name: "blank executable",
			call: llmstream.ToolCall{
				Name:  ToolNameShell,
				Input: `{"command":["","test"]}`,
			},
			expected: ToolNameShell,
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
