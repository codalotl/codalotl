package coretools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/applypatch"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const expectedApplyPatchFreeformDescription = "Use the `apply_patch` tool to edit files. This is a FREEFORM tool, so do not wrap the patch in JSON."

const expectedApplyPatchGrammar = `start: begin_patch hunk+ end_patch
begin_patch: "*** Begin Patch" LF
end_patch: "*** End Patch" LF?

hunk: add_hunk | delete_hunk | update_hunk
add_hunk: "*** Add File: " filename LF add_line+
delete_hunk: "*** Delete File: " filename LF
update_hunk: "*** Update File: " filename LF change_move? change?

filename: /(.+)/
add_line: "+" /(.*)/ LF -> line

change_move: "*** Move to: " filename LF
change: (change_context | change_line)+ eof_line?
change_context: ("@@" | "@@ " /(.+)/) LF
change_line: ("+" | "-" | " ") /(.*)/ LF
eof_line: "*** End of File" LF

%import common.LF`

func TestApplyPatch_Info(t *testing.T) {
	t.Run("freeform", func(t *testing.T) {
		sandbox := t.TempDir()
		auth := authdomain.NewAutoApproveAuthorizer(sandbox)
		tool := NewApplyPatchTool(auth, true, nil)
		info := tool.Info()

		assert.Equal(t, ToolNameApplyPatch, info.Name)
		assert.Equal(t, expectedApplyPatchFreeformDescription, info.Description)
		assert.Equal(t, llmstream.ToolKindCustom, info.Kind)
		require.NotNil(t, info.Grammar)
		assert.Equal(t, llmstream.ToolGrammarSyntaxLark, info.Grammar.Syntax)
		assert.Equal(t, expectedApplyPatchGrammar, info.Grammar.Definition)
		assert.Equal(t, expectedApplyPatchGrammar, applypatch.ApplyPatchGrammar)
		assert.Nil(t, info.Parameters)
	})

	t.Run("function", func(t *testing.T) {
		sandbox := t.TempDir()
		auth := authdomain.NewAutoApproveAuthorizer(sandbox)
		tool := NewApplyPatchTool(auth, false, nil)
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
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)

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
			tool := NewApplyPatchTool(auth, tc.freeform, nil)

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
		selectedTargetDir := filepath.Join(pkg.Module.AbsolutePath, "mypkg")

		postChecks := &ApplyPatchPostChecks{
			TargetDir: selectedTargetDir,
			RunDiagnostics: func(ctx context.Context, sandboxDir string, gotTargetDir string) (string, error) {
				assert.Equal(t, pkg.Module.AbsolutePath, sandboxDir)
				assert.Equal(t, selectedTargetDir, gotTargetDir)
				return diagnosticsOutput, nil
			},
			FixLints: func(ctx context.Context, sandboxDir string, gotTargetDir string) (string, error) {
				assert.Equal(t, pkg.Module.AbsolutePath, sandboxDir)
				assert.Equal(t, selectedTargetDir, gotTargetDir)
				mainFile := filepath.Join(gotTargetDir, "main.go")
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

		auth := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
		tool := NewApplyPatchTool(auth, false, postChecks)

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

func TestApplyPatch_Run_PackageTargetedPostChecks(t *testing.T) {
	tests := []struct {
		name            string
		patch           string
		expectedChecks  []string
		wantDiagnostics bool
	}{
		{
			name: "multi-directory supporting changes",
			patch: `*** Begin Patch
*** Add File: selected/README.txt
+docs
*** Add File: selected/testdata/input.txt
+fixture
*** End Patch
`,
			expectedChecks: []string{"lint"},
		},
		{
			name: "nested supporting-only change",
			patch: `*** Begin Patch
*** Add File: selected/testdata/input.txt
+fixture
*** End Patch
`,
			expectedChecks: []string{"lint"},
		},
		{
			name: "Go change",
			patch: `*** Begin Patch
*** Add File: selected/main.go
+package selected
*** End Patch
`,
			expectedChecks:  []string{"diagnostics", "lint"},
			wantDiagnostics: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			sandbox := t.TempDir()
			targetDir := filepath.Join(sandbox, "selected")
			var checks []string
			postChecks := &ApplyPatchPostChecks{
				TargetDir: targetDir,
				RunDiagnostics: func(ctx context.Context, sandboxDir string, gotTargetDir string) (string, error) {
					assert.Equal(t, sandbox, sandboxDir)
					assert.Equal(t, targetDir, gotTargetDir)
					checks = append(checks, "diagnostics")
					return "<diagnostics-status>diagnostics</diagnostics-status>", nil
				},
				FixLints: func(ctx context.Context, sandboxDir string, gotTargetDir string) (string, error) {
					assert.Equal(t, sandbox, sandboxDir)
					assert.Equal(t, targetDir, gotTargetDir)
					checks = append(checks, "lint")
					return "<lint-status>lint</lint-status>", nil
				},
			}

			auth := authdomain.NewAutoApproveAuthorizer(sandbox)
			call := llmstream.ToolCall{
				CallID: "package-post-checks",
				Name:   ToolNameApplyPatch,
				Type:   "custom_tool_call",
				Input:  tc.patch,
			}
			result := NewApplyPatchTool(auth, true, postChecks).Run(context.Background(), call)

			require.False(t, result.IsError)
			assert.Equal(t, tc.expectedChecks, checks)
			if tc.wantDiagnostics {
				assert.Contains(t, result.Result, "<diagnostics-status>diagnostics</diagnostics-status>")
			} else {
				assert.NotContains(t, result.Result, "<diagnostics-status>")
			}
			assert.Contains(t, result.Result, "<lint-status>lint</lint-status>")
		})
	}
}

func TestApplyPatch_Run_AcceptsAbsolutePaths(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)

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
			tool := NewApplyPatchTool(auth, tc.freeform, nil)
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
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)

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
			tool := NewApplyPatchTool(auth, tc.freeform, nil)
			res := tool.Run(context.Background(), tc.call)
			assert.True(t, res.IsError)
			assert.NotNil(t, res.SourceErr)
			assert.Contains(t, res.Result, "escapes working directory")
		})
	}
}

func TestApplyPatch_Run_PathOutsideSandboxAbsolute(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)

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
			tool := NewApplyPatchTool(auth, tc.freeform, nil)
			res := tool.Run(context.Background(), tc.call)
			assert.True(t, res.IsError)
			assert.NotNil(t, res.SourceErr)
			assert.Contains(t, res.Result, "escapes working directory")
		})
	}
}

func TestApplyPatch_Run_ApplyError(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)

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
			tool := NewApplyPatchTool(auth, tc.freeform, nil)
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
			auth := &stubAuthorizer{sandboxDir: sandbox}
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
			tool := NewApplyPatchTool(auth, tc.freeform, nil)

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

func TestApplyPatch_Run_AuthorizesAllAffectedPaths(t *testing.T) {
	sandbox := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "deleted.txt"), []byte("old\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "updated.txt"), []byte("old\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "moved.txt"), []byte("before\n"), 0o644))

	expectedPaths := []string{
		filepath.Join(sandbox, "added.txt"),
		filepath.Join(sandbox, "deleted.txt"),
		filepath.Join(sandbox, "updated.txt"),
		filepath.Join(sandbox, "moved.txt"),
		filepath.Join(sandbox, "destination.txt"),
	}
	auth := &stubAuthorizer{sandboxDir: sandbox}
	auth.writeResp = func(requestPermission bool, _ string, toolName string, absPath ...string) error {
		assert.False(t, requestPermission)
		assert.Equal(t, ToolNameApplyPatch, toolName)
		assert.Equal(t, expectedPaths, absPath)
		return nil
	}

	patch := strings.Join([]string{
		"  ",
		"  *** Begin Patch  ",
		"  *** Add File: added.txt  ",
		"+new",
		"  *** Delete File: deleted.txt  ",
		"  *** Update File: updated.txt  ",
		"@@",
		"-old",
		"+new",
		"  *** Update File: moved.txt  ",
		"  *** Move to: destination.txt  ",
		"@@",
		"-before",
		"+after",
		"  *** End Patch  ",
		"",
	}, "\n")
	call := llmstream.ToolCall{
		CallID: "auth-all-affected-paths",
		Name:   ToolNameApplyPatch,
		Type:   "custom_tool_call",
		Input:  patch,
	}
	res := NewApplyPatchTool(auth, true, nil).Run(context.Background(), call)
	require.False(t, res.IsError)
	require.Len(t, auth.writeCalls, 1)

	data, err := os.ReadFile(filepath.Join(sandbox, "added.txt"))
	require.NoError(t, err)
	assert.Equal(t, "new\n", string(data))
	data, err = os.ReadFile(filepath.Join(sandbox, "updated.txt"))
	require.NoError(t, err)
	assert.Equal(t, "new\n", string(data))
	data, err = os.ReadFile(filepath.Join(sandbox, "destination.txt"))
	require.NoError(t, err)
	assert.Equal(t, "after\n", string(data))

	_, err = os.Stat(filepath.Join(sandbox, "deleted.txt"))
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(sandbox, "moved.txt"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestApplyPatch_Run_AuthorizesLeadingWhitespaceInPath(t *testing.T) {
	sandbox := t.TempDir()
	expectedPath := filepath.Join(sandbox, " leading.txt")
	auth := &stubAuthorizer{sandboxDir: sandbox}
	auth.writeResp = func(_ bool, _ string, _ string, absPath ...string) error {
		assert.Equal(t, []string{expectedPath}, absPath)
		return nil
	}

	patch := `*** Begin Patch
*** Add File:  leading.txt
+new
*** End Patch
`
	call := llmstream.ToolCall{
		CallID: "auth-leading-whitespace",
		Name:   ToolNameApplyPatch,
		Type:   "custom_tool_call",
		Input:  patch,
	}
	res := NewApplyPatchTool(auth, true, nil).Run(context.Background(), call)
	require.False(t, res.IsError)
	require.Len(t, auth.writeCalls, 1)

	data, err := os.ReadFile(expectedPath)
	require.NoError(t, err)
	assert.Equal(t, "new\n", string(data))
	_, err = os.Stat(filepath.Join(sandbox, "leading.txt"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestApplyPatch_Run_AuthorizesResolvedPathOnce(t *testing.T) {
	sandbox := t.TempDir()
	target := filepath.Join(sandbox, "same.txt")
	auth := &stubAuthorizer{sandboxDir: sandbox}
	auth.writeResp = func(_ bool, _ string, _ string, absPath ...string) error {
		assert.Equal(t, []string{target}, absPath)
		return nil
	}

	patch := fmt.Sprintf(`*** Begin Patch
*** Add File: same.txt
+first
*** Add File: ./same.txt
+second
*** Add File: %s
+third
*** End Patch
`, filepath.ToSlash(target))
	call := llmstream.ToolCall{
		CallID: "auth-path-aliases",
		Name:   ToolNameApplyPatch,
		Type:   "custom_tool_call",
		Input:  patch,
	}
	res := NewApplyPatchTool(auth, true, nil).Run(context.Background(), call)
	require.False(t, res.IsError)
	require.Len(t, auth.writeCalls, 1)

	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "third\n", string(data))
}

func TestApplyPatch_Run_DeniedLaterTargetDoesNotMutate(t *testing.T) {
	sandbox := t.TempDir()
	firstPath := filepath.Join(sandbox, "first.txt")
	secondPath := filepath.Join(sandbox, "second.txt")
	destinationPath := filepath.Join(sandbox, "destination.txt")
	require.NoError(t, os.WriteFile(firstPath, []byte("first before\n"), 0o644))
	require.NoError(t, os.WriteFile(secondPath, []byte("second before\n"), 0o644))

	auth := &stubAuthorizer{sandboxDir: sandbox}
	auth.writeResp = func(_ bool, _ string, _ string, absPath ...string) error {
		assert.Equal(t, []string{firstPath, secondPath, destinationPath}, absPath)
		return fmt.Errorf("destination authorization denied")
	}
	patch := `*** Begin Patch
*** Update File: first.txt
@@
-first before
+first after
*** Update File: second.txt
*** Move to: destination.txt
@@
-second before
+second after
*** End Patch
`
	call := llmstream.ToolCall{
		CallID: "auth-denied-later-target",
		Name:   ToolNameApplyPatch,
		Type:   "custom_tool_call",
		Input:  patch,
	}
	res := NewApplyPatchTool(auth, true, nil).Run(context.Background(), call)
	require.True(t, res.IsError)
	assert.Contains(t, res.Result, "destination authorization denied")
	require.Len(t, auth.writeCalls, 1)

	data, err := os.ReadFile(firstPath)
	require.NoError(t, err)
	assert.Equal(t, "first before\n", string(data))
	data, err = os.ReadFile(secondPath)
	require.NoError(t, err)
	assert.Equal(t, "second before\n", string(data))
	_, err = os.Stat(destinationPath)
	assert.ErrorIs(t, err, os.ErrNotExist)
}
