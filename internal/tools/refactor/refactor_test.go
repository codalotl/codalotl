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
	assert.Contains(t, info.Description, "dry")
}

func TestDocsAddDelegatesToCodalotlCLI(t *testing.T) {
	moduleDir, _ := newTestModule(t)
	var captured docsAddCapture
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
		NewCommandTree: docsAddCommandTree(&captured, "Applied 1 documentation change(s).\n"),
	})

	result := runRefactorTool(t, tool, Params{Name: "docs-add", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	assert.Equal(t, ResultStatusApplied, result.result.Status)
	assert.Equal(t, "internal/foo", result.result.Package)
	assert.True(t, captured.publicOnly)
	assert.Equal(t, []string{"internal/foo"}, captured.args)
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
	found, record := retrieveDryCAS(t, moduleDir, pkgDir)
	assert.True(t, found)
	assert.Equal(t, []string{"helper.go"}, record.Edited)
}

func TestDryCASHitSkipsAgent(t *testing.T) {
	moduleDir, pkgDir := newTestModule(t)
	unit, err := codeunit.DefaultGoCodeUnit(pkgDir)
	require.NoError(t, err)
	require.NoError(t, newCASDB(moduleDir).StoreOnCodeUnit(unit, dryNamespace(), refactorCASRecord{Applied: true}))
	invoker := &fakeAgentInvoker{}
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{
		AgentInvoker: invoker,
	})

	result := runRefactorTool(t, tool, Params{Name: "dry", Package: "internal/foo"})

	require.False(t, result.toolResult.IsError)
	assert.Equal(t, ResultStatusAlreadyApplied, result.result.Status)
	assert.Empty(t, invoker.calls)
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

func TestUnknownNameIsError(t *testing.T) {
	moduleDir, _ := newTestModule(t)
	tool := NewRefactorTool(authdomain.NewAutoApproveAuthorizer(moduleDir), Options{})

	result := runRefactorTool(t, tool, Params{Name: "missing", Package: "internal/foo"})

	assert.True(t, result.toolResult.IsError)
	assert.Contains(t, result.toolResult.Result, "unknown refactor name")
}

type runResult struct {
	toolResult llmstream.ToolResult
	result     Result
}

func runRefactorTool(t *testing.T, tool llmstream.Tool, params Params) runResult {
	t.Helper()

	input, err := json.Marshal(params)
	require.NoError(t, err)
	toolResult := tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call_1",
		Name:   ToolNameRefactor,
		Type:   "function_call",
		Input:  string(input),
	})
	if toolResult.IsError {
		return runResult{toolResult: toolResult}
	}

	var result Result
	require.NoError(t, json.Unmarshal([]byte(toolResult.Result), &result))
	return runResult{toolResult: toolResult, result: result}
}

type docsAddCapture struct {
	publicOnly bool
	args       []string
}

func docsAddCommandTree(capture *docsAddCapture, stdout string) toolcli.CommandTreeFunc {
	return func() *qcli.Command {
		root := &qcli.Command{Name: "codalotl"}
		docs := &qcli.Command{Name: "docs"}
		add := &qcli.Command{Name: "add"}
		publicOnly := add.Flags().Bool("public-only", 0, false, "document only public identifiers")
		add.Run = func(c *qcli.Context) error {
			capture.publicOnly = *publicOnly
			capture.args = append([]string(nil), c.Args...)
			_, err := fmt.Fprint(c.Out, stdout)
			return err
		}
		root.AddCommand(docs)
		docs.AddCommand(add)
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

type fakeAgentInvoker struct {
	onInvoke func(context.Context, string, toolsetinterface.InvokeRequest) error
	calls    []fakeInvokeCall
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
	ch := make(chan agent.Event, 1)
	ch <- agent.Event{Type: agent.EventTypeDoneSuccess}
	close(ch)
	return ch, nil
}

func newTestModule(t *testing.T) (string, string) {
	t.Helper()

	moduleDir := t.TempDir()
	writeFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com/project\n\ngo 1.24.4\n")
	pkgDir := filepath.Join(moduleDir, "internal", "foo")
	writeFile(t, filepath.Join(pkgDir, "foo.go"), "package foo\n\nfunc A() int { return 1 }\n")
	return moduleDir, pkgDir
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func retrieveDryCAS(t *testing.T, moduleDir string, pkgDir string) (bool, refactorCASRecord) {
	t.Helper()

	unit, err := codeunit.DefaultGoCodeUnit(pkgDir)
	require.NoError(t, err)
	var record refactorCASRecord
	found, _, err := newCASDB(moduleDir).RetrieveOnCodeUnit(unit, dryNamespace(), &record)
	require.NoError(t, err)
	return found, record
}

func retrieveDryCASMetadata(t *testing.T, moduleDir string, pkgDir string) (bool, map[string]json.RawMessage) {
	t.Helper()

	unit, err := codeunit.DefaultGoCodeUnit(pkgDir)
	require.NoError(t, err)
	metadata := make(map[string]json.RawMessage)
	found, _, err := newCASDB(moduleDir).RetrieveOnCodeUnit(unit, dryNamespace(), &metadata)
	require.NoError(t, err)
	return found, metadata
}

func dryNamespace() gocas.Namespace {
	return refactorConfig{name: "dry", generation: 1}.casNamespace()
}
