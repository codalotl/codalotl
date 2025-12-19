package coretools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/applypatch"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/llmstream"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyPatch_Info(t *testing.T) {
	t.Run("freeform", func(t *testing.T) {
		tool := NewApplyPatchTool("/sandbox", nil, true, nil)
		info := tool.Info()

		assert.Equal(t, ToolNameApplyPatch, info.Name)
		assert.NotEmpty(t, info.Description)
		assert.Equal(t, llmstream.ToolKindCustom, info.Kind)
		require.NotNil(t, info.Grammar)
		assert.Equal(t, llmstream.ToolGrammarSyntaxLark, info.Grammar.Syntax)
		assert.Equal(t, applypatch.ApplyPatchGrammar, info.Grammar.Definition)
		assert.Nil(t, info.Parameters)
	})

	t.Run("function", func(t *testing.T) {
		tool := NewApplyPatchTool("/sandbox", nil, false, nil)
		info := tool.Info()

		assert.Equal(t, ToolNameApplyPatch, info.Name)
		assert.NotEmpty(t, info.Description)
		assert.Equal(t, llmstream.ToolKindFunction, info.Kind)
		assert.Nil(t, info.Grammar)
		require.NotNil(t, info.Parameters)
		assert.Contains(t, info.Parameters, "patch")
		assert.Equal(t, []string{"patch"}, info.Required)
	})
}

func TestApplyPatch_Run_Success(t *testing.T) {
	sandbox := t.TempDir()

	tests := []struct {
		name      string
		freeform  bool
		callType  string
		buildCall func(patch string) llmstream.ToolCall
	}{
		{
			name:     "freeform",
			freeform: true,
			callType: "custom_tool_call",
			buildCall: func(patch string) llmstream.ToolCall {
				return llmstream.ToolCall{
					CallID: "call-freeform",
					Name:   ToolNameApplyPatch,
					Type:   "custom_tool_call",
					Input:  patch,
				}
			},
		},
		{
			name:     "function",
			freeform: false,
			callType: "function_call",
			buildCall: func(patch string) llmstream.ToolCall {
				payload, err := json.Marshal(map[string]string{"patch": patch})
				require.NoError(t, err)
				return llmstream.ToolCall{
					CallID: "call-function",
					Name:   ToolNameApplyPatch,
					Type:   "function_call",
					Input:  string(payload),
				}
			},
		},
	}

	patch := `*** Begin Patch
*** Add File: hello.txt
+hello
*** End Patch
`

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tool := NewApplyPatchTool(sandbox, nil, tc.freeform, nil)

			wdBefore, err := os.Getwd()
			require.NoError(t, err)

			call := tc.buildCall(patch)
			res := tool.Run(context.Background(), call)
			assert.False(t, res.IsError)
			assert.Nil(t, res.SourceErr)
			assert.Equal(t, tc.callType, res.Type)

			const prefix = "<apply-patch ok=\"true\">"
			const suffix = "</apply-patch>"
			require.True(t, strings.HasPrefix(res.Result, prefix))
			require.True(t, strings.HasSuffix(res.Result, suffix))

			inner := strings.TrimSuffix(strings.TrimPrefix(res.Result, prefix+"\n"), "\n"+suffix)
			lines := strings.Split(inner, "\n")
			require.GreaterOrEqual(t, len(lines), 2)
			assert.Equal(t, "Updated the following files:", lines[0])
			assert.Contains(t, lines[1:], "A hello.txt")

			data, readErr := os.ReadFile(filepath.Join(sandbox, "hello.txt"))
			require.NoError(t, readErr)
			assert.Equal(t, "hello\n", string(data))

			wdAfter, err := os.Getwd()
			require.NoError(t, err)
			assert.Equal(t, wdBefore, wdAfter)
		})

		// cleanup between runs
		require.NoError(t, os.Remove(filepath.Join(sandbox, "hello.txt")))
	}
}

func TestApplyPatch_Run_CheckErrors(t *testing.T) {
	gocodetesting.WithMultiCode(t, map[string]string{
		"main.go": gocodetesting.Dedent(`
			package mypkg

			func main() {}
		`),
	}, func(pkg *gocode.Package) {
		const diagnosticsOutput = "<diagnostics-status ok=\"true\">diag</diagnostics-status>"
		const lintOutput = "<lint-status ok=\"true\">lint</lint-status>"

		postChecks := &ApplyPatchPostChecks{
			RunDiagnostics: func(ctx context.Context, sandboxDir string, targetDir string) (string, error) {
				assert.Equal(t, pkg.Module.AbsolutePath, sandboxDir)
				assert.Equal(t, filepath.Join(pkg.Module.AbsolutePath, "mypkg"), targetDir)
				return diagnosticsOutput, nil
			},
			FixLints: func(ctx context.Context, sandboxDir string, targetDir string) (string, error) {
				assert.Equal(t, pkg.Module.AbsolutePath, sandboxDir)
				assert.Equal(t, filepath.Join(pkg.Module.AbsolutePath, "mypkg"), targetDir)
				mainFile := filepath.Join(targetDir, "main.go")
				data, err := os.ReadFile(mainFile)
				if err != nil {
					return "", err
				}
				formatted, err := format.Source(data)
				if err != nil {
					return "", err
				}
				if writeErr := os.WriteFile(mainFile, formatted, 0o644); writeErr != nil {
					return "", writeErr
				}
				return lintOutput, nil
			},
		}

		tool := NewApplyPatchTool(pkg.Module.AbsolutePath, nil, false, postChecks)

		patch := `*** Begin Patch
*** Update File: mypkg/main.go
@@
-func main() {}
+func main(){
+    println("hi")
+}
*** End Patch
`
		payload, err := json.Marshal(map[string]string{"patch": patch})
		require.NoError(t, err)

		call := llmstream.ToolCall{
			CallID: "call-check-errors",
			Name:   ToolNameApplyPatch,
			Type:   "function_call",
			Input:  string(payload),
		}

		res := tool.Run(context.Background(), call)
		assert.False(t, res.IsError)
		assert.Nil(t, res.SourceErr)
		assert.Equal(t, "function_call", res.Type)

		expected := fmt.Sprintf(`<apply-patch ok="true">
Updated the following files:
M mypkg/main.go
</apply-patch>
%s
%s`, diagnosticsOutput, lintOutput)
		assert.Equal(t, expected, res.Result)

		pkgDir := filepath.Join(pkg.Module.AbsolutePath, "mypkg")
		contents, readErr := os.ReadFile(filepath.Join(pkgDir, "main.go"))
		require.NoError(t, readErr)
		expectedContents := "package mypkg\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
		assert.Equal(t, expectedContents, string(contents))
	})
}

func TestApplyPatch_Run_AcceptsAbsolutePaths(t *testing.T) {
	sandbox := t.TempDir()

	abs := filepath.Join(sandbox, "hello.txt")
	patch := fmt.Sprintf(`*** Begin Patch
*** Add File: %s
+hello
*** End Patch
`, filepath.ToSlash(abs))

	tests := []struct {
		name      string
		freeform  bool
		callType  string
		buildCall func(patch string) llmstream.ToolCall
	}{
		{
			name:     "freeform",
			freeform: true,
			callType: "custom_tool_call",
			buildCall: func(patch string) llmstream.ToolCall {
				return llmstream.ToolCall{
					CallID: "call-freeform-abs",
					Name:   ToolNameApplyPatch,
					Type:   "custom_tool_call",
					Input:  patch,
				}
			},
		},
		{
			name:     "function",
			freeform: false,
			callType: "function_call",
			buildCall: func(patch string) llmstream.ToolCall {
				payload, err := json.Marshal(map[string]string{"patch": patch})
				require.NoError(t, err)
				return llmstream.ToolCall{
					CallID: "call-function-abs",
					Name:   ToolNameApplyPatch,
					Type:   "function_call",
					Input:  string(payload),
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tool := NewApplyPatchTool(sandbox, nil, tc.freeform, nil)
			call := tc.buildCall(patch)
			res := tool.Run(context.Background(), call)
			assert.False(t, res.IsError)
			assert.Nil(t, res.SourceErr)

			data, readErr := os.ReadFile(abs)
			require.NoError(t, readErr)
			assert.Equal(t, "hello\n", string(data))
		})

		require.NoError(t, os.Remove(abs))
	}
}

func TestApplyPatch_Run_PathOutsideSandbox(t *testing.T) {
	sandbox := t.TempDir()

	testPatch := `*** Begin Patch
*** Add File: ../escape.txt
+bad
*** End Patch
`

	tests := []struct {
		name     string
		freeform bool
		call     llmstream.ToolCall
	}{
		{
			name:     "freeform",
			freeform: true,
			call: llmstream.ToolCall{
				CallID: "call-escape-freeform",
				Name:   ToolNameApplyPatch,
				Type:   "custom_tool_call",
				Input:  testPatch,
			},
		},
		{
			name:     "function",
			freeform: false,
			call: func() llmstream.ToolCall {
				payload, err := json.Marshal(map[string]string{"patch": testPatch})
				require.NoError(t, err)
				return llmstream.ToolCall{
					CallID: "call-escape-function",
					Name:   ToolNameApplyPatch,
					Type:   "function_call",
					Input:  string(payload),
				}
			}(),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tool := NewApplyPatchTool(sandbox, nil, tc.freeform, nil)
			res := tool.Run(context.Background(), tc.call)
			assert.True(t, res.IsError)
			assert.NotNil(t, res.SourceErr)
			assert.Contains(t, res.Result, "escapes working directory")
		})
	}
}

func TestApplyPatch_Run_PathOutsideSandboxAbsolute(t *testing.T) {
	sandbox := t.TempDir()

	outside := filepath.Join(sandbox, "..", "escape.txt")
	patch := fmt.Sprintf(`*** Begin Patch
*** Add File: %s
+bad
*** End Patch
`, filepath.ToSlash(outside))

	tests := []struct {
		name     string
		freeform bool
		call     llmstream.ToolCall
	}{
		{
			name:     "freeform",
			freeform: true,
			call: llmstream.ToolCall{
				CallID: "call-escape-abs-freeform",
				Name:   ToolNameApplyPatch,
				Type:   "custom_tool_call",
				Input:  patch,
			},
		},
		{
			name:     "function",
			freeform: false,
			call: func() llmstream.ToolCall {
				payload, err := json.Marshal(map[string]string{"patch": patch})
				require.NoError(t, err)
				return llmstream.ToolCall{
					CallID: "call-escape-abs-function",
					Name:   ToolNameApplyPatch,
					Type:   "function_call",
					Input:  string(payload),
				}
			}(),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tool := NewApplyPatchTool(sandbox, nil, tc.freeform, nil)
			res := tool.Run(context.Background(), tc.call)
			assert.True(t, res.IsError)
			assert.NotNil(t, res.SourceErr)
			assert.Contains(t, res.Result, "escapes working directory")
		})
	}
}

func TestApplyPatch_Run_ApplyError(t *testing.T) {
	sandbox := t.TempDir()

	testPatch := `*** Begin Patch
*** Update File: missing.txt
@@
-hello
+world
*** End Patch
`

	tests := []struct {
		name     string
		freeform bool
		call     llmstream.ToolCall
	}{
		{
			name:     "freeform",
			freeform: true,
			call: llmstream.ToolCall{
				CallID: "call-error-freeform",
				Name:   ToolNameApplyPatch,
				Type:   "custom_tool_call",
				Input:  testPatch,
			},
		},
		{
			name:     "function",
			freeform: false,
			call: func() llmstream.ToolCall {
				payload, err := json.Marshal(map[string]string{"patch": testPatch})
				require.NoError(t, err)
				return llmstream.ToolCall{
					CallID: "call-error-function",
					Name:   ToolNameApplyPatch,
					Type:   "function_call",
					Input:  string(payload),
				}
			}(),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tool := NewApplyPatchTool(sandbox, nil, tc.freeform, nil)
			res := tool.Run(context.Background(), tc.call)
			assert.True(t, res.IsError)
			assert.NotNil(t, res.SourceErr)
			assert.Contains(t, res.Result, "read missing.txt")
		})
	}
}

func TestApplyPatch_Run_Authorization(t *testing.T) {
	patch := `*** Begin Patch
*** Add File: hello.txt
+hello
*** End Patch
`

	type testcase struct {
		name               string
		freeform           bool
		allow              bool
		expectError        bool
		requestsPermission bool
	}

	tests := []testcase{
		{name: "function allowed", freeform: false, allow: true, requestsPermission: true},
		{name: "function denied", freeform: false, allow: false, expectError: true, requestsPermission: true},
		{name: "freeform allowed", freeform: true, allow: true},
		{name: "freeform denied", freeform: true, allow: false, expectError: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			sandbox := t.TempDir()
			auth := &stubAuthorizer{}
			auth.writeResp = func(requestPermission bool, _ string, toolName string, absPath ...string) error {
				assert.Equal(t, ToolNameApplyPatch, toolName)
				expected := filepath.Join(sandbox, "hello.txt")
				require.Equal(t, []string{expected}, absPath)
				assert.Equal(t, tc.requestsPermission, requestPermission)
				if tc.allow {
					return nil
				}
				return fmt.Errorf("apply_patch authorization denied")
			}
			tool := NewApplyPatchTool(sandbox, auth, tc.freeform, nil)

			var call llmstream.ToolCall
			if tc.freeform {
				call = llmstream.ToolCall{
					CallID: "auth-freeform",
					Name:   ToolNameApplyPatch,
					Type:   "custom_tool_call",
					Input:  patch,
				}
			} else {
				payload, err := json.Marshal(map[string]any{"patch": patch, "request_permission": true})
				require.NoError(t, err)
				call = llmstream.ToolCall{
					CallID: "auth-function",
					Name:   ToolNameApplyPatch,
					Type:   "function_call",
					Input:  string(payload),
				}
			}

			res := tool.Run(context.Background(), call)
			if tc.expectError {
				assert.True(t, res.IsError)
				assert.NotNil(t, res.SourceErr)
				assert.Contains(t, res.Result, "apply_patch authorization denied")
			} else {
				assert.False(t, res.IsError)
				assert.Nil(t, res.SourceErr)
			}
			require.Len(t, auth.writeCalls, 1)
		})
	}
}
