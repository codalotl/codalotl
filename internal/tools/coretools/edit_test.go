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

func TestEdit_Run_ReplaceSingleMatch(t *testing.T) {
	sandbox := t.TempDir()
	path := filepath.Join(sandbox, "note.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello world\n"), 0o644))

	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewEditTool(auth)
	call := llmstream.ToolCall{
		CallID: "call1",
		Name:   ToolNameEdit,
		Type:   "function_call",
		Input:  `{"path":"note.txt","old_text":"world","new_text":"codalotl"}`,
	}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)
	assert.Contains(t, res.Result, "Edited file: note.txt")

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hello codalotl\n", string(b))
}

func TestEdit_Run_ReplaceAll(t *testing.T) {
	sandbox := t.TempDir()
	path := filepath.Join(sandbox, "note.txt")
	require.NoError(t, os.WriteFile(path, []byte("foo foo foo"), 0o644))

	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewEditTool(auth)
	call := llmstream.ToolCall{
		CallID: "call2",
		Name:   ToolNameEdit,
		Type:   "function_call",
		Input:  `{"path":"note.txt","old_text":"foo","new_text":"bar","replace_all":true}`,
	}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "bar bar bar", string(b))
}

func TestEdit_Run_PathIsRequired(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewEditTool(auth)
	call := llmstream.ToolCall{
		CallID: "call3",
		Name:   ToolNameEdit,
		Type:   "function_call",
		Input:  `{"old_text":"a","new_text":"b"}`,
	}

	res := tool.Run(context.Background(), call)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Result, "path is required")
}

func TestEdit_Run_OldTextValidation(t *testing.T) {
	sandbox := t.TempDir()
	path := filepath.Join(sandbox, "note.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewEditTool(auth)

	t.Run("old_text required", func(t *testing.T) {
		call := llmstream.ToolCall{
			CallID: "call4a",
			Name:   ToolNameEdit,
			Type:   "function_call",
			Input:  `{"path":"note.txt","new_text":"x"}`,
		}

		res := tool.Run(context.Background(), call)
		assert.True(t, res.IsError)
		assert.Contains(t, res.Result, "old_text is required")
	})

	t.Run("old_text must not be empty", func(t *testing.T) {
		call := llmstream.ToolCall{
			CallID: "call4b",
			Name:   ToolNameEdit,
			Type:   "function_call",
			Input:  `{"path":"note.txt","old_text":"","new_text":"x"}`,
		}

		res := tool.Run(context.Background(), call)
		assert.True(t, res.IsError)
		assert.Contains(t, res.Result, "old_text must not be empty")
	})
}

func TestEdit_Run_NewTextIsRequired(t *testing.T) {
	sandbox := t.TempDir()
	path := filepath.Join(sandbox, "note.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewEditTool(auth)
	call := llmstream.ToolCall{
		CallID: "call5",
		Name:   ToolNameEdit,
		Type:   "function_call",
		Input:  `{"path":"note.txt","old_text":"hello"}`,
	}

	res := tool.Run(context.Background(), call)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Result, "new_text is required")
}

func TestEdit_Run_PathDoesNotExist(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewEditTool(auth)
	call := llmstream.ToolCall{
		CallID: "call6",
		Name:   ToolNameEdit,
		Type:   "function_call",
		Input:  `{"path":"missing.txt","old_text":"a","new_text":"b"}`,
	}

	res := tool.Run(context.Background(), call)
	assert.True(t, res.IsError)
	assert.NotNil(t, res.SourceErr)
	assert.Contains(t, res.Result, "does not exist")
}

func TestEdit_Run_PathIsDirectory(t *testing.T) {
	sandbox := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(sandbox, "adir"), 0o755))

	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tool := NewEditTool(auth)
	call := llmstream.ToolCall{
		CallID: "call7",
		Name:   ToolNameEdit,
		Type:   "function_call",
		Input:  `{"path":"adir","old_text":"a","new_text":"b"}`,
	}

	res := tool.Run(context.Background(), call)
	assert.True(t, res.IsError)
	assert.NotNil(t, res.SourceErr)
	assert.Contains(t, res.Result, "path is a directory")
}

func TestEdit_Run_Authorization(t *testing.T) {
	sandbox := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "outside.txt")
	require.NoError(t, os.WriteFile(outsidePath, []byte("before outside"), 0o644))

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
				require.NoError(t, os.WriteFile(filepath.Join(sandbox, tc.path), []byte("before inside"), 0o644))
			}

			auth := &stubAuthorizer{sandboxDir: sandbox}
			auth.writeResp = func(requestPermission bool, _ string, toolName string, absPath ...string) error {
				assert.Equal(t, ToolNameEdit, toolName)
				assert.True(t, requestPermission)
				require.Equal(t, []string{tc.expectedAuthPath}, absPath)
				if tc.allow {
					return nil
				}
				return fmt.Errorf("edit authorization denied")
			}

			tool := NewEditTool(auth)
			input := fmt.Sprintf(
				`{"path":%q,"old_text":"before","new_text":"after","request_permission":true}`,
				strings.ReplaceAll(tc.path, "\\", "\\\\"),
			)
			call := llmstream.ToolCall{CallID: "auth", Name: ToolNameEdit, Type: "function_call", Input: input}

			res := tool.Run(context.Background(), call)
			if tc.expectError {
				assert.True(t, res.IsError)
				assert.NotNil(t, res.SourceErr)
				assert.Contains(t, res.Result, "edit authorization denied")
			} else {
				assert.False(t, res.IsError)
				assert.Nil(t, res.SourceErr)
			}

			require.Len(t, auth.writeCalls, 1)
		})
	}
}
