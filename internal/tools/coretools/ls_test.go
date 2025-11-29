package coretools

import (
	"context"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmstream"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLs_Run_BasicListingAndFormatting(t *testing.T) {
	sandbox := t.TempDir()

	// Create files and directories, including hidden ones
	require.NoError(t, os.Mkdir(filepath.Join(sandbox, "adir"), 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(sandbox, "cdir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "afile.txt"), []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "bfile.txt"), []byte("b"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, ".hidden"), []byte("secret"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(sandbox, ".hdir"), 0o755))

	tool := NewLsTool(sandbox, nil)
	call := llmstream.ToolCall{CallID: "call1", Name: ToolNameLS, Type: "function_call", Input: `{"path":"."}`}

	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)

	// Expect ok and cwd on the <ls> tag, and the listing content.
	assert.True(t, strings.HasPrefix(res.Result, "<ls "))
	assert.Contains(t, res.Result, `ok="true"`)
	assert.Contains(t, res.Result, `cwd="`+strings.ReplaceAll(sandbox, "\\", "\\\\")+`"`)
	assert.Contains(t, res.Result, "$ ls -1p")
	// Contents (sorted, non-hidden)
	assert.Contains(t, res.Result, "\nadir/\n")
	assert.Contains(t, res.Result, "\nafile.txt\n")
	assert.Contains(t, res.Result, "\nbfile.txt\n")
	assert.Contains(t, res.Result, "\ncdir/\n")
	assert.True(t, strings.HasSuffix(res.Result, "</ls>"))
}

func TestLs_Run_PathDoesNotExist(t *testing.T) {
	sandbox := t.TempDir()
	tool := NewLsTool(sandbox, nil)

	call := llmstream.ToolCall{CallID: "call3", Name: ToolNameLS, Type: "function_call", Input: `{"path":"does-not-exist"}`}
	res := tool.Run(context.Background(), call)
	assert.True(t, res.IsError)
	assert.NotNil(t, res.SourceErr)
	assert.Contains(t, res.Result, "does not exist")
}

func TestLs_Run_PathIsFile(t *testing.T) {
	sandbox := t.TempDir()
	f := filepath.Join(sandbox, "afile.txt")
	require.NoError(t, os.WriteFile(f, []byte("a"), 0o644))

	tool := NewLsTool(sandbox, nil)
	call := llmstream.ToolCall{CallID: "call4", Name: ToolNameLS, Type: "function_call", Input: `{"path":"afile.txt"}`}
	res := tool.Run(context.Background(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)
	assert.Contains(t, res.Result, "\nafile.txt\n")
}

func TestLs_Run_AbsolutePathInsideSandbox(t *testing.T) {
	sandbox := t.TempDir()
	sub := filepath.Join(sandbox, "adir")
	require.NoError(t, os.Mkdir(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "x.txt"), []byte("x"), 0o644))

	tool := NewLsTool(sandbox, nil)
	input := `{"path":"` + strings.ReplaceAll(sub, "\\", "\\\\") + `"}`
	call := llmstream.ToolCall{CallID: "call5", Name: ToolNameLS, Type: "function_call", Input: input}

	res := tool.Run(t.Context(), call)
	assert.False(t, res.IsError)
	assert.Nil(t, res.SourceErr)
	assert.True(t, strings.HasPrefix(res.Result, "<ls "))
	assert.Contains(t, res.Result, `ok="true"`)
	assert.Contains(t, res.Result, `cwd="`+strings.ReplaceAll(sub, "\\", "\\\\")+`"`)
	assert.Contains(t, res.Result, "$ ls -1p")
	assert.Contains(t, res.Result, "\nx.txt\n")
	assert.True(t, strings.HasSuffix(res.Result, "</ls>"))
}

func TestLs_Run_Authorization(t *testing.T) {
	sandbox := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(sandbox, "adir"), 0o755))

	tests := []struct {
		name        string
		path        string
		allow       bool
		expectError bool
	}{
		{name: "allowed", path: ".", allow: true},
		{name: "denied", path: "..", allow: false, expectError: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			auth := &stubAuthorizer{}
			auth.readResp = func(requestPermission bool, _ string, toolName string, sandboxDir string, absPath ...string) error {
				assert.Equal(t, ToolNameLS, toolName)
				assert.Equal(t, sandbox, sandboxDir)
				assert.True(t, requestPermission)
				expected := filepath.Clean(filepath.Join(sandboxDir, tc.path))
				require.Equal(t, []string{expected}, absPath)
				if tc.allow {
					return nil
				}
				return fmt.Errorf("ls authorization denied")
			}
			tool := NewLsTool(sandbox, auth)
			input := fmt.Sprintf(`{"path":%q,"request_permission":true}`, tc.path)
			call := llmstream.ToolCall{CallID: "auth", Name: ToolNameLS, Type: "function_call", Input: input}

			res := tool.Run(context.Background(), call)
			if tc.expectError {
				assert.True(t, res.IsError)
				assert.NotNil(t, res.SourceErr)
				assert.Contains(t, res.Result, "ls authorization denied")
			} else {
				assert.False(t, res.IsError)
				assert.Nil(t, res.SourceErr)
			}

			require.Len(t, auth.readCalls, 1)
			assert.Equal(t, tc.allow, !res.IsError)
		})
	}
}
