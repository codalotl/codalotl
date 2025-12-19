package coretools

import (
	"context"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadFile_Basic_NoLineNumbers(t *testing.T) {
	sandbox := t.TempDir()
	content := "hello\nworld\n"
	file := filepath.Join(sandbox, "afile.txt")
	require.NoError(t, os.WriteFile(file, []byte(content), 0o644))

	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewReadFileTool(auth)
	call := llmstream.ToolCall{CallID: "call1", Name: ToolNameReadFile, Type: "function_call", Input: `{"path":"afile.txt"}`}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)

	open, inner, close := splitTagged(res.Result)
	attrs := parseAttrs(open)
	assert.Equal(t, "afile.txt", attrs["name"])                      // relative to sandbox
	assert.Equal(t, "2", attrs["line-count"])                        // two lines
	assert.Equal(t, strconv.Itoa(len(content)), attrs["byte-count"]) // bytes emitted equals content bytes
	assert.Equal(t, "false", attrs["any-line-truncated"])            // no truncation
	assert.Equal(t, "false", attrs["file-truncated"])                // not truncated

	assert.Equal(t, content, inner)
	assert.Equal(t, "</file>", close)

}

func TestReadFile_WithLineNumbers(t *testing.T) {
	sandbox := t.TempDir()
	content := "hello\nworld\n"
	file := filepath.Join(sandbox, "afile.txt")
	require.NoError(t, os.WriteFile(file, []byte(content), 0o644))

	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewReadFileTool(auth)
	call := llmstream.ToolCall{CallID: "call2", Name: ToolNameReadFile, Type: "function_call", Input: `{"path":"afile.txt","line_numbers":true}`}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)

	open, inner, _ := splitTagged(res.Result)
	attrs := parseAttrs(open)
	assert.Equal(t, "2", attrs["line-count"]) // two lines
	// byte-count excludes line number prefixes; equals original content bytes
	assert.Equal(t, strconv.Itoa(len(content)), attrs["byte-count"])

	lines := nonEmptyLines(inner)
	assert.Equal(t, []string{"1:hello", "2:world"}, lines)
}

func TestReadFile_Authorization(t *testing.T) {
	sandbox := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "afile.txt"), []byte("hello"), 0o644))

	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	require.NoError(t, os.WriteFile(outsideFile, []byte("secret"), 0o644))

	tests := []struct {
		name             string
		path             string
		expectedAuthPath string
		allow            bool
		expectError      bool
	}{
		{name: "allowed", path: "afile.txt", expectedAuthPath: filepath.Join(sandbox, "afile.txt"), allow: true},
		{name: "denied", path: outsideFile, expectedAuthPath: outsideFile, allow: false, expectError: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			auth := &stubAuthorizer{sandboxDir: sandbox}
			auth.readResp = func(requestPermission bool, _ string, toolName string, absPath ...string) error {
				assert.Equal(t, ToolNameReadFile, toolName)
				assert.True(t, requestPermission)
				require.Equal(t, []string{tc.expectedAuthPath}, absPath)
				if tc.allow {
					return nil
				}
				return fmt.Errorf("read authorization denied")
			}

			tool := NewReadFileTool(auth)
			input := fmt.Sprintf(`{"path":%q,"request_permission":true}`, tc.path)
			call := llmstream.ToolCall{CallID: "auth", Name: ToolNameReadFile, Type: "function_call", Input: input}

			res := tool.Run(context.Background(), call)
			if tc.expectError {
				assert.True(t, res.IsError)
				assert.NotNil(t, res.SourceErr)
				assert.Contains(t, res.Result, "read authorization denied")
			} else {
				assert.False(t, res.IsError)
				assert.Nil(t, res.SourceErr)
				assert.Contains(t, res.Result, "hello")
			}

			require.Len(t, auth.readCalls, 1)
			assert.Equal(t, tc.allow, !res.IsError)
		})
	}
}

func TestReadFile_PathIsDirectory(t *testing.T) {
	sandbox := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(sandbox, "adir"), 0o755))

	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewReadFileTool(auth)
	call := llmstream.ToolCall{CallID: "call4", Name: ToolNameReadFile, Type: "function_call", Input: `{"path":"adir"}`}

	res := tool.Run(context.Background(), call)
	assert.True(t, res.IsError)
	assert.NotNil(t, res.SourceErr)
	assert.Contains(t, res.Result, "path is a directory")
}

func TestReadFile_FileTruncatedBySize(t *testing.T) {
	sandbox := t.TempDir()
	// Build > 300KB content across many lines
	var b strings.Builder
	chunk := strings.Repeat("a", 100) + "\n"
	for b.Len() < 300*1024 {
		b.WriteString(chunk)
	}
	file := filepath.Join(sandbox, "big.txt")
	require.NoError(t, os.WriteFile(file, []byte(b.String()), 0o644))

	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewReadFileTool(auth)
	call := llmstream.ToolCall{CallID: "call5", Name: ToolNameReadFile, Type: "function_call", Input: `{"path":"big.txt"}`}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)

	open, inner, _ := splitTagged(res.Result)
	attrs := parseAttrs(open)
	assert.Equal(t, "true", attrs["file-truncated"]) // exceeded 250KB

	// byte-count should be <= 250KB
	bc, convErr := strconv.Atoi(attrs["byte-count"])
	require.NoError(t, convErr)
	assert.LessOrEqual(t, bc, 250*1024)
	assert.Equal(t, bc, len([]byte(inner)))
}

func TestReadFile_AnyLineTruncated(t *testing.T) {
	sandbox := t.TempDir()
	// Single long line > 2000 chars
	content := strings.Repeat("x", 3000) + "\n"
	file := filepath.Join(sandbox, "longline.txt")
	require.NoError(t, os.WriteFile(file, []byte(content), 0o644))

	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewReadFileTool(auth)
	call := llmstream.ToolCall{CallID: "call6", Name: ToolNameReadFile, Type: "function_call", Input: `{"path":"longline.txt"}`}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)

	open, inner, _ := splitTagged(res.Result)
	attrs := parseAttrs(open)
	assert.Equal(t, "true", attrs["any-line-truncated"]) // line exceeded 2000 chars
	assert.Equal(t, "1", attrs["line-count"])            // only one line
	// Inner should end with newline
	assert.True(t, strings.HasSuffix(inner, "\n"))
}

func TestReadFile_LineCap_Truncated(t *testing.T) {
	sandbox := t.TempDir()
	var b strings.Builder
	for i := 0; i < 10050; i++ {
		b.WriteString("x\n")
	}
	file := filepath.Join(sandbox, "manylines.txt")
	require.NoError(t, os.WriteFile(file, []byte(b.String()), 0o644))

	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewReadFileTool(auth)
	call := llmstream.ToolCall{CallID: "call7", Name: ToolNameReadFile, Type: "function_call", Input: `{"path":"manylines.txt"}`}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)

	open, _, _ := splitTagged(res.Result)
	attrs := parseAttrs(open)
	assert.Equal(t, "10000", attrs["line-count"])    // capped
	assert.Equal(t, "true", attrs["file-truncated"]) // indicates truncation due to cap
}

func TestReadFile_AbsolutePath_NameIsRelative(t *testing.T) {
	sandbox := t.TempDir()
	dir := filepath.Join(sandbox, "d")
	require.NoError(t, os.Mkdir(dir, 0o755))
	file := filepath.Join(dir, "x.txt")
	require.NoError(t, os.WriteFile(file, []byte("a\n"), 0o644))

	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewReadFileTool(auth)
	input := `{"path":"` + strings.ReplaceAll(file, "\\", "\\\\") + `"}`
	call := llmstream.ToolCall{CallID: "call8", Name: ToolNameReadFile, Type: "function_call", Input: input}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)

	open, _, _ := splitTagged(res.Result)
	attrs := parseAttrs(open)
	assert.Equal(t, "d/x.txt", attrs["name"]) // relative inside sandbox
}

func TestReadFile_NonUTF8_TrailingTrim(t *testing.T) {
	sandbox := t.TempDir()
	// Write invalid UTF-8 bytes followed by valid content
	data := []byte{0xff, 0xfe, 'A', '\n'}
	file := filepath.Join(sandbox, "bin.dat")
	require.NoError(t, os.WriteFile(file, data, 0o644))

	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewReadFileTool(auth)
	call := llmstream.ToolCall{CallID: "call9", Name: ToolNameReadFile, Type: "function_call", Input: `{"path":"bin.dat"}`}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)

	open, inner, _ := splitTagged(res.Result)
	attrs := parseAttrs(open)
	assert.Equal(t, "true", attrs["file-truncated"]) // invalid bytes trimmed
	assert.Contains(t, inner, "A\n")                 // preserved valid tail
}

// Helpers
func splitTagged(s string) (openTag string, inner string, closeTag string) {
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return s, "", ""
	}
	open := lines[0]
	close := lines[len(lines)-2]
	inner = strings.Join(lines[1:len(lines)-2], "\n")
	if inner != "" {
		inner += "\n"
	}
	return open, inner, close
}

func parseAttrs(openTag string) map[string]string {
	attrs := make(map[string]string)
	openTag = strings.TrimSpace(openTag)
	if !strings.HasPrefix(openTag, "<file ") {
		return attrs
	}
	// Strip leading "<file " and trailing ">"
	body := strings.TrimSuffix(strings.TrimPrefix(openTag, "<file "), ">")
	parts := strings.Fields(body)
	for _, p := range parts {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := kv[0]
		val := strings.Trim(kv[1], "\"")
		attrs[key] = val
	}
	return attrs
}

func nonEmptyLines(s string) []string {
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
