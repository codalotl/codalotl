package coretools

import (
	"context"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDelete_Run_DeleteFile(t *testing.T) {
	sandbox := t.TempDir()
	path := filepath.Join(sandbox, "note.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewDeleteTool(auth)
	call := llmstream.ToolCall{
		CallID: "call1",
		Name:   ToolNameDelete,
		Type:   "function_call",
		Input:  `{"path":"note.txt"}`,
	}
	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)
	assert.Contains(t, res.Result, "Deleted file: note.txt")
	_, err := os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist)
}
func TestDelete_Run_PathDoesNotExist(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewDeleteTool(auth)
	call := llmstream.ToolCall{
		CallID: "call2",
		Name:   ToolNameDelete,
		Type:   "function_call",
		Input:  `{"path":"missing.txt"}`,
	}
	res := tool.Run(context.Background(), call)
	assert.True(t, res.IsError)
	assert.NotNil(t, res.SourceErr)
	assert.Contains(t, res.Result, "does not exist")
}
func TestDelete_Run_PathIsDirectory(t *testing.T) {
	sandbox := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(sandbox, "adir"), 0o755))
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewDeleteTool(auth)
	call := llmstream.ToolCall{
		CallID: "call3",
		Name:   ToolNameDelete,
		Type:   "function_call",
		Input:  `{"path":"adir"}`,
	}
	res := tool.Run(context.Background(), call)
	assert.True(t, res.IsError)
	assert.NotNil(t, res.SourceErr)
	assert.Contains(t, res.Result, "path is a directory")
}
func TestDelete_Run_PathIsRequired(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewDeleteTool(auth)
	call := llmstream.ToolCall{
		CallID: "call4",
		Name:   ToolNameDelete,
		Type:   "function_call",
		Input:  `{}`,
	}
	res := tool.Run(context.Background(), call)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Result, "path is required")
}
func TestDelete_Run_Authorization(t *testing.T) {
	sandbox := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "outside.txt")
	require.NoError(t, os.WriteFile(outsidePath, []byte("outside"), 0o644))
	tests := []struct {
		name             string
		path             string
		expectedAuthPath string
		allow            bool
		expectError      bool
	}{
		{
			name:             "allowed in sandbox",
			path:             "in.txt",
			expectedAuthPath: filepath.Join(sandbox, "in.txt"),
			allow:            true,
		},
		{
			name:             "denied outside sandbox",
			path:             outsidePath,
			expectedAuthPath: outsidePath,
			allow:            false,
			expectError:      true,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.path == "in.txt" {
				require.NoError(t, os.WriteFile(filepath.Join(sandbox, tc.path), []byte("inside"), 0o644))
			}
			auth := &stubAuthorizer{sandboxDir: sandbox}
			auth.writeResp = func(requestPermission bool, _ string, toolName string, absPath ...string) error {
				assert.Equal(t, ToolNameDelete, toolName)
				assert.True(t, requestPermission)
				require.Equal(t, []string{tc.expectedAuthPath}, absPath)
				if tc.allow {
					return nil
				}
				return fmt.Errorf("delete authorization denied")
			}
			tool := NewDeleteTool(auth)
			input := fmt.Sprintf(`{"path":%q,"request_permission":true}`, strings.ReplaceAll(tc.path, "\\", "\\\\"))
			call := llmstream.ToolCall{CallID: "auth", Name: ToolNameDelete, Type: "function_call", Input: input}
			res := tool.Run(context.Background(), call)
			if tc.expectError {
				assert.True(t, res.IsError)
				assert.NotNil(t, res.SourceErr)
				assert.Contains(t, res.Result, "delete authorization denied")
			} else {
				assert.False(t, res.IsError)
				assert.Nil(t, res.SourceErr)
			}
			require.Len(t, auth.writeCalls, 1)
		})
	}
}
