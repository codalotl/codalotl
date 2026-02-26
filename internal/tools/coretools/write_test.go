package coretools

import (
	"context"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrite_Run_CreateFile(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewWriteTool(auth)

	call := llmstream.ToolCall{
		CallID: "call1",
		Name:   ToolNameWrite,
		Type:   "function_call",
		Input:  `{"path":"note.txt","content":"hello\n"}`,
	}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)
	assert.Contains(t, res.Result, "Wrote file: note.txt")

	b, err := os.ReadFile(filepath.Join(sandbox, "note.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(b))
}

func TestWrite_Run_OverwriteFile(t *testing.T) {
	sandbox := t.TempDir()
	path := filepath.Join(sandbox, "note.txt")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))

	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewWriteTool(auth)
	call := llmstream.ToolCall{
		CallID: "call2",
		Name:   ToolNameWrite,
		Type:   "function_call",
		Input:  `{"path":"note.txt","content":"new content"}`,
	}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new content", string(b))
}

func TestWrite_Run_EmptyContentIsAllowed(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewWriteTool(auth)
	call := llmstream.ToolCall{
		CallID: "call3",
		Name:   ToolNameWrite,
		Type:   "function_call",
		Input:  `{"path":"empty.txt","content":""}`,
	}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)

	b, err := os.ReadFile(filepath.Join(sandbox, "empty.txt"))
	require.NoError(t, err)
	assert.Equal(t, "", string(b))
}

func TestWrite_Run_CreatesParentDirectories(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewWriteTool(auth)
	call := llmstream.ToolCall{
		CallID: "call4",
		Name:   ToolNameWrite,
		Type:   "function_call",
		Input:  `{"path":"a/b/c.txt","content":"abc"}`,
	}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)

	b, err := os.ReadFile(filepath.Join(sandbox, "a", "b", "c.txt"))
	require.NoError(t, err)
	assert.Equal(t, "abc", string(b))
}

func TestWrite_Run_PathIsDirectory(t *testing.T) {
	sandbox := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(sandbox, "adir"), 0o755))

	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewWriteTool(auth)
	call := llmstream.ToolCall{
		CallID: "call5",
		Name:   ToolNameWrite,
		Type:   "function_call",
		Input:  `{"path":"adir","content":"x"}`,
	}

	res := tool.Run(context.Background(), call)
	assert.True(t, res.IsError)
	assert.NotNil(t, res.SourceErr)
	assert.Contains(t, res.Result, "path is a directory")
}

func TestWrite_Run_ContentIsRequired(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewWriteTool(auth)
	call := llmstream.ToolCall{
		CallID: "call6",
		Name:   ToolNameWrite,
		Type:   "function_call",
		Input:  `{"path":"x.txt"}`,
	}

	res := tool.Run(context.Background(), call)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Result, "content is required")
}

func TestWrite_Run_Authorization(t *testing.T) {
	sandbox := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "outside.txt")

	tests := []struct {
		name             string
		path             string
		content          string
		expectedAuthPath string
		allow            bool
		expectError      bool
	}{
		{
			name:             "allowed in sandbox",
			path:             "in.txt",
			content:          "ok",
			expectedAuthPath: filepath.Join(sandbox, "in.txt"),
			allow:            true,
		},
		{
			name:             "denied outside sandbox",
			path:             outsidePath,
			content:          "nope",
			expectedAuthPath: outsidePath,
			allow:            false,
			expectError:      true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			auth := &stubAuthorizer{sandboxDir: sandbox}
			auth.writeResp = func(requestPermission bool, _ string, toolName string, absPath ...string) error {
				assert.Equal(t, ToolNameWrite, toolName)
				assert.True(t, requestPermission)
				require.Equal(t, []string{tc.expectedAuthPath}, absPath)
				if tc.allow {
					return nil
				}
				return fmt.Errorf("write authorization denied")
			}

			tool := NewWriteTool(auth)
			input := fmt.Sprintf(
				`{"path":%q,"content":%q,"request_permission":true}`,
				strings.ReplaceAll(tc.path, "\\", "\\\\"),
				tc.content,
			)
			call := llmstream.ToolCall{CallID: "auth", Name: ToolNameWrite, Type: "function_call", Input: input}

			res := tool.Run(context.Background(), call)
			if tc.expectError {
				assert.True(t, res.IsError)
				assert.NotNil(t, res.SourceErr)
				assert.Contains(t, res.Result, "write authorization denied")
			} else {
				assert.False(t, res.IsError)
				assert.Nil(t, res.SourceErr)
			}

			require.Len(t, auth.writeCalls, 1)
		})
	}
}
