package refactor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocas/casclarify"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmstream"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	toolcli "github.com/codalotl/codalotl/internal/tools/cli"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInfo(t *testing.T) {
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), Options{})

	info := tool.Info()

	assert.Equal(t, ToolNameRefactor, info.Name)
	assert.Equal(t, []string{"name", "package"}, info.Required)
	assert.Contains(t, info.Parameters, "name")
	assert.Contains(t, info.Parameters, "package")
	assert.Contains(t, info.Description, "docs-add")
	assert.Contains(t, info.Description, "important")
	assert.Contains(t, info.Description, "docs-fix")
	assert.Contains(t, info.Description, "materially false")
	assert.Contains(t, info.Description, "docs-improve-from-clarify")
	assert.Contains(t, info.Description, "clarify_public_api")
	assert.Contains(t, info.Description, "dry")
	assert.Contains(t, info.Description, "test-cleanup")
	assert.Contains(t, info.Description, "existing Go tests")
	assert.Contains(t, info.Description, "without adding missing coverage")
	assert.Contains(t, info.Description, "test-ensure-coverage")
	assert.Contains(t, info.Description, "public APIs")
	assert.Contains(t, info.Description, "important edge cases")
}

func TestToolIdentity(t *testing.T) {
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(t.TempDir()), Options{})

	assert.Equal(t, ToolNameRefactor, tool.Name())
	assert.IsType(t, refactorPresenter{}, tool.Presenter())
}

func TestDocsAddDelegatesToCodalotlCLI(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	var captured docsAddCapture
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
		NewCommandTree: docsAddCommandTree(&captured, "Applied 1 documentation change(s).\n"),
	})

	result := runRefactorTool(t, tool, Params{Name: "docs-add", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	assert.Equal(t, ResultStatusApplied, result.result.Status)
	assert.Equal(t, "internal/foo", result.result.Package)
	require.NotNil(t, result.result.EditedFiles)
	assert.Empty(t, result.result.EditedFiles)
	assert.Nil(t, result.result.SavedCASRecord)
	assertJSONOmitsField(t, result.toolResult.Result, "saved-cas-record")
	assert.True(t, captured.important)
	assert.Equal(t, []string{pkgDir}, captured.args)
}

func TestDocsAddReportsEditedFiles(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	var captured docsAddCapture
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
		NewCommandTree: docsAddEditingCommandTree(&captured, pkgDir),
	})

	result := runRefactorTool(t, tool, Params{Name: "docs-add", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	assert.Equal(t, ResultStatusApplied, result.result.Status)
	assert.Equal(t, []string{"doc.go"}, result.result.EditedFiles)
	assert.Nil(t, result.result.SavedCASRecord)
	assertJSONOmitsField(t, result.toolResult.Result, "saved-cas-record")
	assert.Equal(t, []string{pkgDir}, captured.args)
}

func TestDocsAddNoOpportunity(t *testing.T) {
	moduleDir, _ := newTestModule(t)
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
		NewCommandTree: docsAddCommandTree(&docsAddCapture{}, "Nothing left to document!\n"),
	})

	result := runRefactorTool(t, tool, Params{Name: "docs-add", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	assert.Equal(t, ResultStatusNoOpportunity, result.result.Status)
	assert.Equal(t, "no refactoring opportunities found", result.result.Message)
	require.NotNil(t, result.result.EditedFiles)
	assert.Empty(t, result.result.EditedFiles)
	assert.Nil(t, result.result.SavedCASRecord)
	assertJSONOmitsField(t, result.toolResult.Result, "saved-cas-record")
}

func TestDocsAddIgnoresCASRecordAndReportsActualResult(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	unit, err := codeunit.DefaultGoCodeUnit(pkgDir)
	require.NoError(t, err)
	require.NoError(t, newTestCASDB(t, moduleDir).StoreOnCodeUnit(unit, refactorConfig{name: "docs-add"}.casNamespace(), refactorCASRecord{Applied: true}))
	var captured docsAddCapture
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
		NewCommandTree: docsAddCommandTree(&captured, "Nothing left to document!\n"),
	})

	result := runRefactorTool(t, tool, Params{Name: "docs-add", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	assert.Equal(t, ResultStatusNoOpportunity, result.result.Status)
	assert.Equal(t, "no refactoring opportunities found", result.result.Message)
	require.NotNil(t, result.result.EditedFiles)
	assert.Empty(t, result.result.EditedFiles)
	assert.Nil(t, result.result.SavedCASRecord)
	assert.Equal(t, []string{pkgDir}, captured.args)
}

func TestDocsAddIgnoresCASRecordAndReportsDelegateError(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	unit, err := codeunit.DefaultGoCodeUnit(pkgDir)
	require.NoError(t, err)
	require.NoError(t, newTestCASDB(t, moduleDir).StoreOnCodeUnit(unit, refactorConfig{name: "docs-add"}.casNamespace(), refactorCASRecord{Applied: true}))
	var captured docsAddCapture
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
		NewCommandTree: failingDocsAddCommandTree(&captured, errors.New("delegate failed")),
	})

	result := runRefactorTool(t, tool, Params{Name: "docs-add", Package: "internal/foo"})

	assert.True(t, result.toolResult.IsError)
	assert.Contains(t, result.toolResult.Result, "delegate failed")
	assert.Equal(t, []string{pkgDir}, captured.args)
}

func TestDocsFixDelegatesToCodalotlCLIAndReportsEditedFiles(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	var captured docsFixCapture
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
		NewCommandTree: docsFixEditingCommandTree(&captured, pkgDir),
	})

	result := runRefactorTool(t, tool, Params{Name: "docs-fix", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	assert.Equal(t, ResultStatusApplied, result.result.Status)
	assert.Equal(t, "internal/foo", result.result.Package)
	assert.Equal(t, []string{"foo.go"}, result.result.EditedFiles)
	assert.Nil(t, result.result.SavedCASRecord)
	assertJSONOmitsField(t, result.toolResult.Result, "saved-cas-record")
	assert.Equal(t, []string{pkgDir}, captured.args)
}

func TestDocsFixNoOpportunityOmitsCASRecord(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	var captured docsFixCapture
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
		NewCommandTree: docsFixCommandTree(&captured, "Checked documentation.\n"),
	})

	result := runRefactorTool(t, tool, Params{Name: "docs-fix", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	assert.Equal(t, ResultStatusNoOpportunity, result.result.Status)
	assert.Equal(t, "no refactoring opportunities found", result.result.Message)
	require.NotNil(t, result.result.EditedFiles)
	assert.Empty(t, result.result.EditedFiles)
	assert.Nil(t, result.result.SavedCASRecord)
	assertJSONOmitsField(t, result.toolResult.Result, "saved-cas-record")
	assert.Equal(t, []string{pkgDir}, captured.args)
}

func TestDocsFixIgnoresRefactorCASRecord(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	unit, err := codeunit.DefaultGoCodeUnit(pkgDir)
	require.NoError(t, err)
	require.NoError(t, newTestCASDB(t, moduleDir).StoreOnCodeUnit(unit, refactorConfig{name: "docs-fix"}.casNamespace(), refactorCASRecord{Applied: true}))
	var captured docsFixCapture
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
		NewCommandTree: docsFixCommandTree(&captured, "Checked documentation.\n"),
	})

	result := runRefactorTool(t, tool, Params{Name: "docs-fix", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	assert.Equal(t, ResultStatusNoOpportunity, result.result.Status)
	assert.Nil(t, result.result.SavedCASRecord)
	assertJSONOmitsField(t, result.toolResult.Result, "saved-cas-record")
	assert.Equal(t, []string{pkgDir}, captured.args)
}

func TestDocsImproveFromClarifyNoRelevantEntriesSkipsAgent(t *testing.T) {
	moduleDir, _ := newTestModule(t)
	recordPath := newTestClarifyRecordFile(t, moduleDir)
	stubFindInPlayClarifyRecords(t, func(*gocas.DB, *gocode.Module) ([]casclarify.InPlayRecord, error) {
		return []casclarify.InPlayRecord{
			{
				Path:          recordPath,
				TargetPackage: "example.com/project/internal/bar",
				Metadata: casclarify.Metadata{
					Entries: []casclarify.Entry{
						{
							TargetPackage: "example.com/project/internal/bar",
							Identifier:    "Bar",
							Question:      "How does Bar work?",
							Answer:        "It works elsewhere.",
						},
					},
				},
			},
		}, nil
	})
	invoker := &fakeAgentInvoker{}
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
		AgentInvoker: invoker,
	})

	result := runRefactorTool(t, tool, Params{Name: "docs-improve-from-clarify", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	assert.Equal(t, ResultStatusNoOpportunity, result.result.Status)
	require.NotNil(t, result.result.EditedFiles)
	assert.Empty(t, result.result.EditedFiles)
	assert.Nil(t, result.result.SavedCASRecord)
	assert.Empty(t, invoker.calls)
	assert.FileExists(t, recordPath)
}

func TestDocsImproveFromClarifyNoRelevantEntriesDoesNotRequireAgentInvoker(t *testing.T) {
	moduleDir, _ := newTestModule(t)
	stubFindInPlayClarifyRecords(t, func(*gocas.DB, *gocode.Module) ([]casclarify.InPlayRecord, error) {
		return nil, nil
	})
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{})

	result := runRefactorTool(t, tool, Params{Name: "docs-improve-from-clarify", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	assert.Equal(t, ResultStatusNoOpportunity, result.result.Status)
	assert.Empty(t, result.result.EditedFiles)
}

func TestDocsImproveFromClarifyInvokesPackageAgentAndDeletesRecordsOnNoEdits(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	db := newTestCASDB(t, moduleDir)
	recordPath := newTestClarifyRecordFile(t, moduleDir)
	stubFindInPlayClarifyRecords(t, func(gotDB *gocas.DB, mod *gocode.Module) ([]casclarify.InPlayRecord, error) {
		assert.Equal(t, db.BaseDir, gotDB.BaseDir)
		assert.Equal(t, moduleDir, mod.AbsolutePath)
		return []casclarify.InPlayRecord{
			{
				Path:          recordPath,
				TargetPackage: "example.com/project/internal/foo",
				Metadata: casclarify.Metadata{
					Entries: []casclarify.Entry{
						{
							OriginPackage: "example.com/project/internal/caller",
							Identifier:    "Client.Do",
							Question:      "Should callers reuse Client?",
							Answer:        "Clients are safe for concurrent reuse.",
						},
					},
				},
			},
		}, nil
	})
	invoker := &fakeAgentInvoker{}
	authorizer := &recordingAuthorizer{
		Authorizer: authdomain.NewAutoApproveAuthorizer(moduleDir),
	}
	tool := NewRefactorTool(authorizer, Options{
		AgentInvoker: invoker,
	})

	result := runRefactorTool(t, tool, Params{Name: "docs-improve-from-clarify", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	assert.Equal(t, ResultStatusNoOpportunity, result.result.Status)
	assert.Equal(t, "internal/foo", result.result.Package)
	require.NotNil(t, result.result.EditedFiles)
	assert.Empty(t, result.result.EditedFiles)
	assert.Nil(t, result.result.SavedCASRecord)
	assertJSONOmitsField(t, result.toolResult.Result, "saved-cas-record")
	require.Len(t, invoker.calls, 1)
	assert.Equal(t, "package_mode_default_context", invoker.calls[0].agentName)
	assert.Equal(t, pkgDir, invoker.calls[0].req.ToolOptions.GoPkgAbsDir)
	prompt := invoker.calls[0].req.Messages[0]
	assert.Contains(t, prompt, "clarify_public_api")
	assert.Contains(t, prompt, "package docs")
	assert.Contains(t, prompt, "related type docs")
	assert.Contains(t, prompt, "Do not blindly paste answers")
	assert.Contains(t, prompt, "documentation-only public-doc improvements")
	assert.Contains(t, prompt, "Should callers reuse Client?")
	assert.Contains(t, prompt, "Clients are safe for concurrent reuse.")
	assert.NoFileExists(t, recordPath)
	assert.Contains(t, authorizer.writePaths, db.AbsRoot)
}

func TestDocsImproveFromClarifyReportsEditedFiles(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	recordPath := newTestClarifyRecordFile(t, moduleDir)
	stubFindInPlayClarifyRecords(t, func(*gocas.DB, *gocode.Module) ([]casclarify.InPlayRecord, error) {
		return []casclarify.InPlayRecord{
			{
				Path:          recordPath,
				TargetPackage: "example.com/project/internal/foo",
				Metadata: casclarify.Metadata{
					Entries: []casclarify.Entry{
						{
							Identifier: "Client",
							Question:   "Where should timeout behavior be documented?",
							Answer:     "Package docs should explain timeout behavior.",
						},
					},
				},
			},
		}, nil
	})
	invoker := &fakeAgentInvoker{
		onInvoke: func(context.Context, string, toolsetinterface.InvokeRequest) error {
			writeFile(t, filepath.Join(pkgDir, "doc.go"), "package foo\n\n// Package foo explains timeout behavior.\n")
			return nil
		},
	}
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
		AgentInvoker: invoker,
	})

	result := runRefactorTool(t, tool, Params{Name: "docs-improve-from-clarify", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	assert.Equal(t, ResultStatusApplied, result.result.Status)
	assert.Equal(t, []string{"doc.go"}, result.result.EditedFiles)
	assert.Nil(t, result.result.SavedCASRecord)
	assert.NoFileExists(t, recordPath)
}

func TestDocsImproveFromClarifyPreservesRecordsOnAgentFailure(t *testing.T) {
	moduleDir, _ := newTestModule(t)
	recordPath := newTestClarifyRecordFile(t, moduleDir)
	stubFindInPlayClarifyRecords(t, func(*gocas.DB, *gocode.Module) ([]casclarify.InPlayRecord, error) {
		return []casclarify.InPlayRecord{
			{
				Path:          recordPath,
				TargetPackage: "example.com/project/internal/foo",
				Metadata: casclarify.Metadata{
					Entries: []casclarify.Entry{
						{
							Identifier: "Client",
							Question:   "Should Client be reused?",
							Answer:     "Yes.",
						},
					},
				},
			},
		}, nil
	})
	invoker := &fakeAgentInvoker{
		onInvoke: func(context.Context, string, toolsetinterface.InvokeRequest) error {
			return errors.New("agent failed")
		},
	}
	authorizer := &recordingAuthorizer{
		Authorizer: authdomain.NewAutoApproveAuthorizer(moduleDir),
	}
	tool := NewRefactorTool(authorizer, Options{
		AgentInvoker: invoker,
	})

	result := runRefactorTool(t, tool, Params{Name: "docs-improve-from-clarify", Package: "internal/foo"})

	assert.True(t, result.toolResult.IsError)
	assert.Contains(t, result.toolResult.Result, "agent failed")
	assert.FileExists(t, recordPath)
	assert.Empty(t, authorizer.writePaths)
}

func TestDryNoOpportunityWritesPostRunCAS(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	invoker := &fakeAgentInvoker{}
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
		AgentInvoker: invoker,
	})

	result := runRefactorTool(t, tool, Params{Name: "dry", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	assert.Equal(t, ResultStatusNoOpportunity, result.result.Status)
	require.NotNil(t, result.result.EditedFiles)
	assert.Empty(t, result.result.EditedFiles)
	require.NotNil(t, result.result.SavedCASRecord)
	assert.False(t, filepath.IsAbs(*result.result.SavedCASRecord))
	assert.FileExists(t, filepath.Join(moduleDir, filepath.FromSlash(*result.result.SavedCASRecord)))
	require.Len(t, invoker.calls, 1)
	assert.Equal(t, "limited_package_mode", invoker.calls[0].agentName)
	assert.Equal(t, pkgDir, invoker.calls[0].req.ToolOptions.GoPkgAbsDir)
	assert.Contains(t, invoker.calls[0].req.Messages[0], "DRY up this Go package.")
	assert.Contains(t, invoker.calls[0].req.Messages[0], "Target package: `internal/foo`.")

	found, record := retrieveDryCAS(t, moduleDir, pkgDir)
	assert.True(t, found)
	assert.True(t, record.Applied)
	assert.Empty(t, record.Edited)

	found, metadata := retrieveDryCASMetadata(t, moduleDir, pkgDir)
	assert.True(t, found)
	assert.Contains(t, metadata, "edited")
	assert.JSONEq(t, `[]`, string(metadata["edited"]))
}

func TestDryInvokesAgentWithTargetPackageAuthorizer(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	invoker := &fakeAgentInvoker{
		onInvoke: func(_ context.Context, _ string, req toolsetinterface.InvokeRequest) error {
			requirePackageAuthorizer(t, req.ToolOptions.Authorizer, moduleDir, pkgDir)
			requirePackageAuthorizer(t, req.CallerAuthorizer, moduleDir, pkgDir)
			assert.Equal(t, moduleDir, req.ToolOptions.SandboxDir)
			assert.Equal(t, moduleDir, req.CallerSandboxDir)
			return nil
		},
	}
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
		AgentInvoker: invoker,
	})

	result := runRefactorTool(t, tool, Params{Name: "dry", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	require.Len(t, invoker.calls, 1)
	assert.Equal(t, "limited_package_mode", invoker.calls[0].agentName)
}

func TestDryDetectsEditedFilesAndWritesCAS(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	invoker := &fakeAgentInvoker{
		onInvoke: func(context.Context, string, toolsetinterface.InvokeRequest) error {
			writeFile(t, filepath.Join(pkgDir, "helper.go"), "package foo\n\nfunc helper() int { return 2 }\n")
			return nil
		},
	}
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
		AgentInvoker: invoker,
	})

	result := runRefactorTool(t, tool, Params{Name: "dry", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	assert.Equal(t, ResultStatusApplied, result.result.Status)
	assert.Equal(t, []string{"helper.go"}, result.result.EditedFiles)
	require.NotNil(t, result.result.SavedCASRecord)
	assert.Contains(t, *result.result.SavedCASRecord, ".codalotl/cas/refactor-dry-1/")
	found, record := retrieveDryCAS(t, moduleDir, pkgDir)
	assert.True(t, found)
	assert.Equal(t, []string{"helper.go"}, record.Edited)
}

func TestPromptRefactorReportsAgentTerminalFailures(t *testing.T) {
	tests := []struct {
		name   string
		events []agent.Event
		want   string
	}{
		{
			name: "error event detail",
			events: []agent.Event{
				{Type: agent.EventTypeError, Error: errors.New("agent exploded")},
			},
			want: "agent exploded",
		},
		{
			name: "error event fallback",
			events: []agent.Event{
				{Type: agent.EventTypeError},
			},
			want: "prompt refactor agent failed",
		},
		{
			name: "canceled event fallback",
			events: []agent.Event{
				{Type: agent.EventTypeCanceled},
			},
			want: context.Canceled.Error(),
		},
		{
			name:   "closed without terminal event",
			events: []agent.Event{},
			want:   "prompt refactor agent ended without success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			moduleDir, _ := newTestModule(t)
			invoker := &fakeAgentInvoker{
				useEvents: true,
				events:    tt.events,
			}
			tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
				AgentInvoker: invoker,
			})

			result := runRefactorTool(t, tool, Params{Name: "dry", Package: "internal/foo"})

			assert.True(t, result.toolResult.IsError)
			assert.Contains(t, result.toolResult.Result, tt.want)
			require.Len(t, invoker.calls, 1)
		})
	}
}

func TestDryUsesSelectedEnvCASRoot(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	selectedRoot := filepath.Join(moduleDir, "custom-cas")
	t.Setenv(gocas.EnvCASDB, selectedRoot)
	invoker := &fakeAgentInvoker{}
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
		AgentInvoker: invoker,
	})

	result := runRefactorTool(t, tool, Params{Name: "dry", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	require.NotNil(t, result.result.SavedCASRecord)
	assert.Contains(t, *result.result.SavedCASRecord, "custom-cas/refactor-dry-1/")
	assert.NotContains(t, *result.result.SavedCASRecord, ".codalotl/cas")
	assert.FileExists(t, filepath.Join(moduleDir, filepath.FromSlash(*result.result.SavedCASRecord)))
	assert.NoDirExists(t, filepath.Join(moduleDir, ".codalotl", "cas"))

	db := newTestCASDB(t, moduleDir)
	assert.Equal(t, selectedRoot, db.AbsRoot)
	found, record := retrieveDryCAS(t, moduleDir, pkgDir)
	assert.True(t, found)
	assert.True(t, record.Applied)
}

func TestDryUsesSelectedEnvCASRootOutsideSandboxWhenAuthorizerAllows(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	selectedRoot := filepath.Join(t.TempDir(), "cas")
	t.Setenv(gocas.EnvCASDB, selectedRoot)
	invoker := &fakeAgentInvoker{}
	authorizer := &recordingAuthorizer{
		Authorizer: authdomain.NewAutoApproveAuthorizer(moduleDir),
	}
	tool := NewRefactorTool(authorizer, Options{
		AgentInvoker: invoker,
	})

	result := runRefactorTool(t, tool, Params{Name: "dry", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	require.NotNil(t, result.result.SavedCASRecord)
	assert.True(t, filepath.IsAbs(filepath.FromSlash(*result.result.SavedCASRecord)))
	assert.Contains(t, *result.result.SavedCASRecord, filepath.ToSlash(selectedRoot)+"/refactor-dry-1/")
	assert.FileExists(t, filepath.FromSlash(*result.result.SavedCASRecord))
	assert.Contains(t, authorizer.readPaths, selectedRoot)
	assert.Contains(t, authorizer.writePaths, selectedRoot)
	require.Len(t, invoker.calls, 1)

	found, record := retrieveDryCAS(t, moduleDir, pkgDir)
	assert.True(t, found)
	assert.True(t, record.Applied)
}

func TestDryAllowsDefaultCASRootOutsideSandboxWhenAuthorizerAllows(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	invoker := &fakeAgentInvoker{}
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(pkgDir), Options{
		AgentInvoker: invoker,
	})

	result := runRefactorTool(t, tool, Params{Name: "dry", Package: pkgDir})

	require.False(t, result.toolResult.IsError)
	require.NotNil(t, result.result.SavedCASRecord)
	assert.Contains(t, *result.result.SavedCASRecord, ".codalotl/cas/refactor-dry-1/")
	assert.FileExists(t, filepath.Join(moduleDir, filepath.FromSlash(*result.result.SavedCASRecord)))
	require.Len(t, invoker.calls, 1)

	found, record := retrieveDryCAS(t, moduleDir, pkgDir)
	assert.True(t, found)
	assert.True(t, record.Applied)
}

func TestDryRejectsCASRootWhenAuthorizerDeniesRead(t *testing.T) {
	moduleDir, _ := newTestModule(t)
	db := newTestCASDB(t, moduleDir)
	invoker := &fakeAgentInvoker{}
	authorizer := &recordingAuthorizer{
		Authorizer:   authdomain.NewAutoApproveAuthorizer(moduleDir),
		denyReadPath: db.AbsRoot,
	}
	tool := NewRefactorTool(authorizer, Options{
		AgentInvoker: invoker,
	})

	result := runRefactorTool(t, tool, Params{Name: "dry", Package: "internal/foo"})

	assert.True(t, result.toolResult.IsError)
	assert.Contains(t, result.toolResult.Result, "cas read denied")
	assert.Contains(t, authorizer.readPaths, db.AbsRoot)
	assert.NotContains(t, authorizer.writePaths, db.AbsRoot)
	assert.Empty(t, invoker.calls)
}

func TestDryCASHitSkipsAgent(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	unit, err := codeunit.DefaultGoCodeUnit(pkgDir)
	require.NoError(t, err)
	require.NoError(t, newTestCASDB(t, moduleDir).StoreOnCodeUnit(unit, dryNamespace(), refactorCASRecord{Applied: true}))
	invoker := &fakeAgentInvoker{}
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
		AgentInvoker: invoker,
	})

	result := runRefactorTool(t, tool, Params{Name: "dry", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	assert.Equal(t, ResultStatusAlreadyApplied, result.result.Status)
	require.NotNil(t, result.result.EditedFiles)
	assert.Empty(t, result.result.EditedFiles)
	assert.Nil(t, result.result.SavedCASRecord)
	assert.Empty(t, invoker.calls)
}

func TestPromptRefactorNoOpportunityInvokesAgentWithPromptAndWritesCAS(t *testing.T) {
	tests := []struct {
		name           string
		refactorName   string
		namespace      gocas.Namespace
		setup          func(*testing.T, string)
		promptContains []string
	}{
		{
			name:         "test cleanup",
			refactorName: "test-cleanup",
			namespace:    testCleanupNamespace(),
			setup:        writeUncleanFooTest,
			promptContains: []string{
				"Use the `$go-testing` skill",
				"existing Go tests",
				"Remove or coalesce redundant tests",
				"testing helpers",
				"table-driven form",
				"Do not add missing test coverage",
				"Do not make marginal edits",
				"Target package: `internal/foo`.",
			},
		},
		{
			name:         "test ensure coverage",
			refactorName: "test-ensure-coverage",
			namespace:    testEnsureCoverageNamespace(),
			promptContains: []string{
				"Use the `$go-testing` skill",
				"go test -coverprofile",
				"go tool cover -func",
				"Ensure the public API is tested",
				"important edge cases",
				"Do not primarily reorganize",
				"Do not edit non-test code",
				"Target package: `internal/foo`.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			moduleDir, pkgDir := newTestModule(t)
			if tt.setup != nil {
				tt.setup(t, pkgDir)
			}
			invoker := &fakeAgentInvoker{}
			tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
				AgentInvoker: invoker,
			})

			result := runRefactorTool(t, tool, Params{Name: tt.refactorName, Package: "internal/foo"})

			require.False(t, result.toolResult.IsError)
			assert.Equal(t, ResultStatusNoOpportunity, result.result.Status)
			assert.Equal(t, "internal/foo", result.result.Package)
			require.NotNil(t, result.result.EditedFiles)
			assert.Empty(t, result.result.EditedFiles)
			require.NotNil(t, result.result.SavedCASRecord)
			assert.Contains(t, *result.result.SavedCASRecord, fmt.Sprintf(".codalotl/cas/refactor-%s-1/", tt.refactorName))
			assert.FileExists(t, filepath.Join(moduleDir, filepath.FromSlash(*result.result.SavedCASRecord)))
			require.Len(t, invoker.calls, 1)
			assert.Equal(t, "limited_package_mode", invoker.calls[0].agentName)
			assert.Equal(t, pkgDir, invoker.calls[0].req.ToolOptions.GoPkgAbsDir)
			assertContainsAll(t, invoker.calls[0].req.Messages[0], tt.promptContains)

			found, record := retrieveRefactorCAS(t, moduleDir, pkgDir, tt.namespace)
			assert.True(t, found)
			assert.True(t, record.Applied)
			assert.Empty(t, record.Edited)

			found, metadata := retrieveRefactorCASMetadata(t, moduleDir, pkgDir, tt.namespace)
			assert.True(t, found)
			assert.Contains(t, metadata, "edited")
			assert.JSONEq(t, `[]`, string(metadata["edited"]))
		})
	}
}

func TestPromptRefactorDetectsEditedFilesAndWritesCAS(t *testing.T) {
	tests := []struct {
		name         string
		refactorName string
		namespace    gocas.Namespace
		setup        func(*testing.T, string)
		edit         func(*testing.T, string)
	}{
		{
			name:         "test cleanup",
			refactorName: "test-cleanup",
			namespace:    testCleanupNamespace(),
			setup:        writeUncleanFooTest,
			edit: func(t *testing.T, pkgDir string) {
				writeFile(t, filepath.Join(pkgDir, "foo_test.go"), cleanFooTestContent)
			},
		},
		{
			name:         "test ensure coverage",
			refactorName: "test-ensure-coverage",
			namespace:    testEnsureCoverageNamespace(),
			edit: func(t *testing.T, pkgDir string) {
				writeFile(t, filepath.Join(pkgDir, "foo_test.go"), coverageFooTestContent)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			moduleDir, pkgDir := newTestModule(t)
			if tt.setup != nil {
				tt.setup(t, pkgDir)
			}
			invoker := &fakeAgentInvoker{
				onInvoke: func(context.Context, string, toolsetinterface.InvokeRequest) error {
					tt.edit(t, pkgDir)
					return nil
				},
			}
			tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
				AgentInvoker: invoker,
			})

			result := runRefactorTool(t, tool, Params{Name: tt.refactorName, Package: "internal/foo"})

			require.False(t, result.toolResult.IsError)
			assert.Equal(t, ResultStatusApplied, result.result.Status)
			assert.Equal(t, []string{"foo_test.go"}, result.result.EditedFiles)
			require.NotNil(t, result.result.SavedCASRecord)
			assert.Contains(t, *result.result.SavedCASRecord, fmt.Sprintf(".codalotl/cas/refactor-%s-1/", tt.refactorName))
			found, record := retrieveRefactorCAS(t, moduleDir, pkgDir, tt.namespace)
			assert.True(t, found)
			assert.Equal(t, []string{"foo_test.go"}, record.Edited)
		})
	}
}

func TestPresenterUsesSemanticSummaryRoles(t *testing.T) {
	call := refactorToolCall(t, Params{Name: "dry", Package: "internal/foo"})

	presentation := refactorPresenter{}.Present(call, nil)

	assert.Equal(t, llmstream.CompletionBehaviorAppend, presentation.Behavior)
	assert.Equal(t, llmstream.Line{
		JoinWithSpace: true,
		Segments: []llmstream.Segment{
			{Text: "Refactoring", Role: llmstream.RoleAction},
			{Text: "dry", Role: llmstream.RoleNormal},
			{Text: "in", Role: llmstream.RoleAccent},
			{Text: "internal/foo", Role: llmstream.RoleNormal},
		},
	}, presentation.Summary)
}

func TestPresenterCompleteIncludesStatusDetail(t *testing.T) {
	call := refactorToolCall(t, Params{Name: "dry", Package: "internal/foo"})
	payload, err := json.Marshal(Result{
		Name:           "dry",
		Package:        "internal/tools/refactor",
		Status:         ResultStatusAlreadyApplied,
		Message:        "refactor already applied",
		EditedFiles:    []string{},
		SavedCASRecord: nil,
	})
	require.NoError(t, err)
	result := llmstream.ToolResult{
		CallID: "call_1",
		Name:   ToolNameRefactor,
		Type:   "function_call",
		Result: string(payload),
	}

	presentation := refactorPresenter{}.Present(call, &result)

	assert.Equal(t, llmstream.Line{
		JoinWithSpace: true,
		Segments: []llmstream.Segment{
			{Text: "Refactored", Role: llmstream.RoleAction},
			{Text: "dry", Role: llmstream.RoleNormal},
			{Text: "in", Role: llmstream.RoleAccent},
			{Text: "internal/tools/refactor", Role: llmstream.RoleNormal},
		},
	}, presentation.Summary)
	paragraph, ok := presentation.Body.(llmstream.Paragraph)
	require.True(t, ok)
	assert.Equal(t, llmstream.Paragraph{
		Lines: []llmstream.Line{
			{
				Segments: []llmstream.Segment{
					{Text: "Refactor already applied", Role: llmstream.RoleAccent},
				},
			},
		},
	}, paragraph)
}

func TestResolvePackageAcceptsCurrentModuleImportPath(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)

	resolved, err := resolvePackage(authdomain.NewAutoApproveAuthorizer(moduleDir), "example.com/project/internal/foo")

	require.NoError(t, err)
	assert.Equal(t, pkgDir, resolved.absDir)
	assert.Equal(t, "internal/foo", resolved.relDir)
}

func TestResolvePackageRejectsOutsideCurrentModule(t *testing.T) {
	moduleDir, _ := newTestModule(t)
	outsideDir := t.TempDir()
	writeFile(t, filepath.Join(outsideDir, "outside.go"), "package outside\n")
	auth := authdomain.NewAutoApproveAuthorizer(moduleDir)

	_, stdlibErr := resolvePackage(auth, "fmt")
	_, outsideErr := resolvePackage(auth, outsideDir)

	assert.Error(t, stdlibErr)
	assert.Error(t, outsideErr)
}

func TestResolvePackageRejectsCurrentModulePackageOutsideSandbox(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	barDir := filepath.Join(moduleDir, "internal", "bar")
	writeFile(t, filepath.Join(barDir, "bar.go"), "package bar\n")
	auth := authdomain.NewAutoApproveAuthorizer(pkgDir)

	_, err := resolvePackage(auth, "internal/bar")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the sandbox")
}

func TestChangedFilesDetectsAddedAndDeletedEmptyFiles(t *testing.T) {
	assert.Equal(t, []string{"empty.go"}, changedFiles(codeUnitSnapshot{}, codeUnitSnapshot{
		"empty.go": []byte{},
	}))
	assert.Equal(t, []string{"empty.go"}, changedFiles(codeUnitSnapshot{
		"empty.go": []byte{},
	}, codeUnitSnapshot{}))
	assert.Empty(t, changedFiles(codeUnitSnapshot{
		"empty.go": []byte{},
	}, codeUnitSnapshot{
		"empty.go": []byte{},
	}))
}

func TestRelPathInsideRejectsParentEscapesWithEitherSeparator(t *testing.T) {
	assert.False(t, relPathInside(".."))
	assert.False(t, relPathInside("../sibling"))
	assert.False(t, relPathInside(`..\sibling`))
	assert.True(t, relPathInside("."))
	assert.True(t, relPathInside("..sibling"))
	assert.True(t, relPathInside(filepath.Join("child", "pkg")))
}

func TestUnknownNameIsError(t *testing.T) {
	moduleDir, _ := newTestModule(t)
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{})

	result := runRefactorTool(t, tool, Params{Name: "missing", Package: "internal/foo"})

	assert.True(t, result.toolResult.IsError)
	assert.Contains(t, result.toolResult.Result, "unknown refactor name")
}

func TestRunRejectsInvalidParams(t *testing.T) {
	tool := NewRefactorTool(nil, Options{})
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "invalid JSON",
			input: `{"name":`,
			want:  "unexpected EOF",
		},
		{
			name:  "unknown field",
			input: `{"name":"dry","package":"internal/foo","extra":true}`,
			want:  "unknown field",
		},
		{
			name:  "multiple JSON values",
			input: `{"name":"dry","package":"internal/foo"} {}`,
			want:  "multiple JSON values",
		},
		{
			name:  "missing name",
			input: `{"package":"internal/foo"}`,
			want:  `missing required field "name"`,
		},
		{
			name:  "missing package",
			input: `{"name":"dry"}`,
			want:  `missing required field "package"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.Run(context.Background(), llmstream.ToolCall{
				CallID: "call_1",
				Name:   ToolNameRefactor,
				Type:   "function_call",
				Input:  tt.input,
			})

			assert.True(t, result.IsError)
			assert.Equal(t, ToolNameRefactor, result.Name)
			assert.Equal(t, "call_1", result.CallID)
			assert.Contains(t, result.Result, tt.want)
		})
	}
}

type runResult struct {
	toolResult llmstream.ToolResult
	result     Result
}

func runRefactorTool(t *testing.T, tool llmstream.Tool, params Params) runResult {
	t.Helper()

	toolResult := tool.Run(context.Background(), refactorToolCall(t, params))
	if toolResult.IsError {
		return runResult{toolResult: toolResult}
	}

	var result Result
	require.NoError(t, json.Unmarshal([]byte(toolResult.Result), &result))
	return runResult{toolResult: toolResult, result: result}
}

func refactorToolCall(t *testing.T, params Params) llmstream.ToolCall {
	t.Helper()

	input, err := json.Marshal(params)
	require.NoError(t, err)
	return llmstream.ToolCall{
		CallID: "call_1",
		Name:   ToolNameRefactor,
		Type:   "function_call",
		Input:  string(input),
	}
}

func assertJSONOmitsField(t *testing.T, payload string, field string) {
	t.Helper()

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(payload), &fields))
	_, ok := fields[field]
	assert.False(t, ok)
}

func assertContainsAll(t *testing.T, s string, substrings []string) {
	t.Helper()

	for _, substring := range substrings {
		assert.Contains(t, s, substring)
	}
}

type docsAddCapture struct {
	important bool
	args      []string
}

func docsAddCommandTree(capture *docsAddCapture, stdout string) toolcli.CommandTreeFunc {
	return docsAddCommandTreeFunc(capture, func(c *qcli.Context) error {
		_, err := fmt.Fprint(c.Out, stdout)
		return err
	})
}

func docsAddEditingCommandTree(capture *docsAddCapture, pkgDir string) toolcli.CommandTreeFunc {
	return docsAddCommandTreeFunc(capture, func(c *qcli.Context) error {
		if err := os.WriteFile(filepath.Join(pkgDir, "doc.go"), []byte("package foo\n\n// B returns 2.\nfunc B() int { return 2 }\n"), 0o644); err != nil {
			return err
		}
		_, err := fmt.Fprint(c.Out, "Applied 1 documentation change(s).\n")
		return err
	})
}

func failingDocsAddCommandTree(capture *docsAddCapture, runErr error) toolcli.CommandTreeFunc {
	return docsAddCommandTreeFunc(capture, func(c *qcli.Context) error {
		_, err := fmt.Fprint(c.Err, runErr.Error())
		if err != nil {
			return err
		}
		return runErr
	})
}

func docsAddCommandTreeFunc(capture *docsAddCapture, run func(*qcli.Context) error) toolcli.CommandTreeFunc {
	return func() *qcli.Command {
		root := &qcli.Command{Name: "codalotl"}
		docs := &qcli.Command{Name: "docs"}
		add := &qcli.Command{Name: "add"}
		important := add.Flags().Bool("important", 0, false, "document only important identifiers")
		add.Run = func(c *qcli.Context) error {
			capture.important = *important
			capture.args = append([]string(nil), c.Args...)
			return run(c)
		}
		root.AddCommand(docs)
		docs.AddCommand(add)
		return root
	}
}

type docsFixCapture struct {
	args []string
}

func docsFixCommandTree(capture *docsFixCapture, stdout string) toolcli.CommandTreeFunc {
	return docsFixCommandTreeFunc(capture, func(c *qcli.Context) error {
		_, err := fmt.Fprint(c.Out, stdout)
		return err
	})
}

func docsFixEditingCommandTree(capture *docsFixCapture, pkgDir string) toolcli.CommandTreeFunc {
	return docsFixCommandTreeFunc(capture, func(c *qcli.Context) error {
		if err := os.WriteFile(filepath.Join(pkgDir, "foo.go"), []byte("package foo\n\n// A returns 1.\nfunc A() int { return 1 }\n"), 0o644); err != nil {
			return err
		}
		_, err := fmt.Fprint(c.Out, "Checked documentation.\n")
		return err
	})
}

func docsFixCommandTreeFunc(capture *docsFixCapture, run func(*qcli.Context) error) toolcli.CommandTreeFunc {
	return func() *qcli.Command {
		root := &qcli.Command{Name: "codalotl"}
		docs := &qcli.Command{Name: "docs"}
		fix := &qcli.Command{Name: "fix"}
		fix.Run = func(c *qcli.Context) error {
			capture.args = append([]string(nil), c.Args...)
			return run(c)
		}
		root.AddCommand(docs)
		docs.AddCommand(fix)
		return root
	}
}

func requirePackageAuthorizer(t *testing.T, authorizer authdomain.Authorizer, moduleDir string, pkgDir string) {
	t.Helper()

	require.NotNil(t, authorizer)
	assert.True(t, authorizer.IsCodeUnitDomain())
	assert.Equal(t, pkgDir, authorizer.CodeUnitDir())
	assert.Equal(t, moduleDir, authorizer.SandboxDir())

	fallback := authorizer.WithoutCodeUnit()
	require.NotNil(t, fallback)
	assert.False(t, fallback.IsCodeUnitDomain())
	assert.Equal(t, moduleDir, fallback.SandboxDir())
	assert.NoError(t, authorizer.IsAuthorizedForRead(false, "", ToolNameRefactor, filepath.Join(pkgDir, "foo.go")))
}

func stubFindInPlayClarifyRecords(t *testing.T, fn func(*gocas.DB, *gocode.Module) ([]casclarify.InPlayRecord, error)) {
	t.Helper()

	old := findInPlayClarifyRecords
	findInPlayClarifyRecords = fn
	t.Cleanup(func() {
		findInPlayClarifyRecords = old
	})
}

func newTestClarifyRecordFile(t *testing.T, moduleDir string) string {
	t.Helper()

	recordPath := filepath.Join(newTestCASDB(t, moduleDir).AbsRoot, string(casclarify.Namespace), "ab", "record")
	writeFile(t, recordPath, "{}")
	return recordPath
}

type fakeAgentInvoker struct {
	onInvoke  func(context.Context, string, toolsetinterface.InvokeRequest) error
	useEvents bool
	events    []agent.Event
	calls     []fakeInvokeCall
}

type fakeInvokeCall struct {
	agentName string
	req       toolsetinterface.InvokeRequest
}

func (f *fakeAgentInvoker) Create(context.Context, string, toolsetinterface.InvokeRequest) (*agent.Agent, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeAgentInvoker) Invoke(ctx context.Context, agentName string, req toolsetinterface.InvokeRequest) (<-chan agent.Event, error) {
	f.calls = append(f.calls, fakeInvokeCall{agentName: agentName, req: req})
	if f.onInvoke != nil {
		if err := f.onInvoke(ctx, agentName, req); err != nil {
			return nil, err
		}
	}
	if f.useEvents {
		ch := make(chan agent.Event, len(f.events))
		for _, event := range f.events {
			ch <- event
		}
		close(ch)
		return ch, nil
	}
	ch := make(chan agent.Event, 1)
	ch <- agent.Event{Type: agent.EventTypeDoneSuccess}
	close(ch)
	return ch, nil
}

type recordingAuthorizer struct {
	authdomain.Authorizer
	readPaths     []string
	writePaths    []string
	denyReadPath  string
	denyWritePath string
}

func (a *recordingAuthorizer) IsAuthorizedForRead(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	a.readPaths = append(a.readPaths, absPath...)
	if containsPath(absPath, a.denyReadPath) {
		return errors.New("cas read denied")
	}
	return a.Authorizer.IsAuthorizedForRead(requestPermission, requestReason, toolName, absPath...)
}

func (a *recordingAuthorizer) IsAuthorizedForWrite(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	a.writePaths = append(a.writePaths, absPath...)
	if containsPath(absPath, a.denyWritePath) {
		return errors.New("cas write denied")
	}
	return a.Authorizer.IsAuthorizedForWrite(requestPermission, requestReason, toolName, absPath...)
}

func containsPath(paths []string, target string) bool {
	if target == "" {
		return false
	}
	for _, path := range paths {
		if path == target {
			return true
		}
	}
	return false
}

func newTestModule(t *testing.T) (string, string) {
	t.Helper()

	unsetEnv(t, gocas.EnvCASDB)
	moduleDir := t.TempDir()
	writeFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com/project\n\ngo 1.24.4\n")
	require.NoError(t, os.Mkdir(filepath.Join(moduleDir, ".git"), 0o755))
	pkgDir := filepath.Join(moduleDir, "internal", "foo")
	writeFile(t, filepath.Join(pkgDir, "foo.go"), "package foo\n\nfunc A() int { return 1 }\n")
	return moduleDir, pkgDir
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

const uncleanFooTestContent = `package foo

import "testing"

func TestA(t *testing.T) {
	if A() != 1 {
		t.Fatal("bad")
	}
}
`

const cleanFooTestContent = `package foo

import "testing"

func TestA(t *testing.T) {
	got := A()
	if got != 1 {
		t.Fatalf("A() = %d", got)
	}
}
`

const coverageFooTestContent = `package foo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestA(t *testing.T) {
	assert.Equal(t, 1, A())
}
`

func writeUncleanFooTest(t *testing.T, pkgDir string) {
	t.Helper()

	writeFile(t, filepath.Join(pkgDir, "foo_test.go"), uncleanFooTestContent)
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()

	old, hadOld := os.LookupEnv(key)
	require.NoError(t, os.Unsetenv(key))
	t.Cleanup(func() {
		var err error
		if hadOld {
			err = os.Setenv(key, old)
		} else {
			err = os.Unsetenv(key)
		}
		require.NoError(t, err)
	})
}

func retrieveDryCAS(t *testing.T, moduleDir string, pkgDir string) (bool, refactorCASRecord) {
	t.Helper()

	return retrieveRefactorCAS(t, moduleDir, pkgDir, dryNamespace())
}

func retrieveDryCASMetadata(t *testing.T, moduleDir string, pkgDir string) (bool, map[string]json.RawMessage) {
	t.Helper()

	return retrieveRefactorCASMetadata(t, moduleDir, pkgDir, dryNamespace())
}

func retrieveRefactorCAS(t *testing.T, moduleDir string, pkgDir string, namespace gocas.Namespace) (bool, refactorCASRecord) {
	t.Helper()

	unit, err := codeunit.DefaultGoCodeUnit(pkgDir)
	require.NoError(t, err)
	var record refactorCASRecord
	found, _, err := newTestCASDB(t, moduleDir).RetrieveOnCodeUnit(unit, namespace, &record)
	require.NoError(t, err)
	return found, record
}

func retrieveRefactorCASMetadata(t *testing.T, moduleDir string, pkgDir string, namespace gocas.Namespace) (bool, map[string]json.RawMessage) {
	t.Helper()

	unit, err := codeunit.DefaultGoCodeUnit(pkgDir)
	require.NoError(t, err)
	metadata := make(map[string]json.RawMessage)
	found, _, err := newTestCASDB(t, moduleDir).RetrieveOnCodeUnit(unit, namespace, &metadata)
	require.NoError(t, err)
	return found, metadata
}

func newTestCASDB(t *testing.T, moduleDir string) *gocas.DB {
	t.Helper()

	db, err := gocas.NewDBForBaseDir(moduleDir)
	require.NoError(t, err)
	return db
}

func dryNamespace() gocas.Namespace {
	return refactorConfig{name: "dry", generation: 1}.casNamespace()
}

func testCleanupNamespace() gocas.Namespace {
	return refactorConfig{name: "test-cleanup", generation: 1}.casNamespace()
}

func testEnsureCoverageNamespace() gocas.Namespace {
	return refactorConfig{name: "test-ensure-coverage", generation: 1}.casNamespace()
}
