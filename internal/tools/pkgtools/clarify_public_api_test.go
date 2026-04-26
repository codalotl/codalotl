package pkgtools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type denyReadAuthorizer struct {
	sandboxDir string
	readCalls  []string
}

func TestClarifyPublicAPITool_ExposesPresenter(t *testing.T) {
	sandbox := t.TempDir()
	tool := NewClarifyPublicAPITool(authdomain.NewAutoApproveAuthorizer(sandbox), nil)

	assert.NotNil(t, tool.Presenter())
}

func TestClarifyPublicAPIPresenter(t *testing.T) {
	sandbox := t.TempDir()
	tool := NewClarifyPublicAPITool(authdomain.NewAutoApproveAuthorizer(sandbox), nil)
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  ToolNameClarifyPublicAPI,
		Input: `{"path":"axi/some/pkg","identifier":"SomeIdentifier","question":"What does SomeIdentifier return?"}`,
	}
	payload, err := json.Marshal(map[string]any{
		"success": true,
		"content": "SomeIdentifier returns a description and a nil error.",
	})
	require.NoError(t, err)
	result := &llmstream.ToolResult{
		Name:   ToolNameClarifyPublicAPI,
		Result: string(payload),
	}

	callPresentation := presenter.Present(call, nil)
	resultPresentation := presenter.Present(call, result)

	finalMessagePresenter, ok := presenter.(llmstream.SubagentFinalMessagePresenter)
	require.True(t, ok)
	assert.Nil(t, finalMessagePresenter.SubagentFinalMessage(call, "clarify subagent", "done"))
	assert.Equal(t, llmstream.CompletionBehaviorAppend, callPresentation.Behavior)
	assert.Equal(t, llmstream.CompletionBehaviorAppend, resultPresentation.Behavior)
	assert.Equal(t, llmstream.Line{
		JoinWithSpace: true,
		Segments: []llmstream.Segment{
			{Text: "Clarifying API", Role: llmstream.RoleAction},
			{Text: "SomeIdentifier", Role: llmstream.RoleNormal},
			{Text: "in", Role: llmstream.RoleAccent},
			{Text: "axi/some/pkg", Role: llmstream.RoleNormal},
		},
	}, callPresentation.Summary)
	assert.Equal(t, llmstream.Line{
		JoinWithSpace: true,
		Segments: []llmstream.Segment{
			{Text: "Clarified API", Role: llmstream.RoleAction},
			{Text: "SomeIdentifier", Role: llmstream.RoleNormal},
			{Text: "in", Role: llmstream.RoleAccent},
			{Text: "axi/some/pkg", Role: llmstream.RoleNormal},
		},
	}, resultPresentation.Summary)
	assert.Equal(t, llmstream.Output{
		Lines: []string{"What does SomeIdentifier return?"},
	}, callPresentation.Body)
	assert.Equal(t, llmstream.Output{
		Lines: []string{"SomeIdentifier returns a description and a nil error."},
	}, resultPresentation.Body)
}

func TestClarifyPublicAPIPresenter_PreservesRawJSONObjectResult(t *testing.T) {
	sandbox := t.TempDir()
	tool := NewClarifyPublicAPITool(authdomain.NewAutoApproveAuthorizer(sandbox), nil)
	presenter := tool.Presenter()

	require.NotNil(t, presenter)

	call := llmstream.ToolCall{
		Name:  ToolNameClarifyPublicAPI,
		Input: `{"path":"axi/some/pkg","identifier":"SomeIdentifier","question":"What does SomeIdentifier return?"}`,
	}
	result := &llmstream.ToolResult{
		Name:   ToolNameClarifyPublicAPI,
		Result: `{"answer":"SomeIdentifier returns a description."}`,
	}

	presentation := presenter.Present(call, result)

	assert.Equal(t, llmstream.Output{
		Lines: []string{`{"answer":"SomeIdentifier returns a description."}`},
	}, presentation.Body)
}

func TestClarifyPublicAPIPresenterResultContent_PreservesRawJSONObject(t *testing.T) {
	content, ok := clarifyPublicAPIPresenterResultContent(llmstream.ToolResult{
		Result: `{"answer":"SomeIdentifier returns a description."}`,
	})

	assert.True(t, ok)
	assert.Equal(t, `{"answer":"SomeIdentifier returns a description."}`, content)
}

func (a *denyReadAuthorizer) SandboxDir() string { return a.sandboxDir }
func (a *denyReadAuthorizer) CodeUnitDir() string {
	return ""
}
func (a *denyReadAuthorizer) IsCodeUnitDomain() bool { return false }
func (a *denyReadAuthorizer) WithoutCodeUnit() authdomain.Authorizer {
	return a
}
func (a *denyReadAuthorizer) IsAuthorizedForRead(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	a.readCalls = append(a.readCalls, absPath...)
	return errors.New("deny read")
}
func (a *denyReadAuthorizer) IsAuthorizedForWrite(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	return nil
}
func (a *denyReadAuthorizer) IsShellAuthorized(requestPermission bool, requestReason string, cwd string, command []string) error {
	return nil
}
func (a *denyReadAuthorizer) Close() {}

func TestClarifyPublicAPI_RunRelativePackagePathRequestsAuth(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		auth := &denyReadAuthorizer{sandboxDir: pkg.Module.AbsolutePath}
		tool := NewClarifyPublicAPITool(auth, nil)
		call := llmstream.ToolCall{
			CallID: "call-relative",
			Name:   ToolNameClarifyPublicAPI,
			Type:   "function_call",
			Input:  `{"path":"mypkg","identifier":"Hello","question":"What does Hello return?"}`,
		}

		res := tool.Run(context.Background(), call)
		assert.True(t, res.IsError)
		assert.Contains(t, res.Result, "deny read")
		assert.NotEmpty(t, auth.readCalls)
		assert.Equal(t, pkg.AbsolutePath(), auth.readCalls[0])
	})
}

func TestClarifyPublicAPI_RunDependencyImportDoesNotRequestAuth(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	assert.True(t, ok)

	mod, err := gocode.NewModule(thisFile)
	if !assert.NoError(t, err) {
		return
	}

	auth := &denyReadAuthorizer{sandboxDir: mod.AbsolutePath}
	tool := NewClarifyPublicAPITool(auth, nil)
	call := llmstream.ToolCall{
		CallID: "call-dep",
		Name:   ToolNameClarifyPublicAPI,
		Type:   "function_call",
		Input:  `{"path":"github.com/stretchr/testify/assert","identifier":"Equal","question":"What does Equal do?"}`,
	}

	res := tool.Run(context.Background(), call)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Result, "unable to create subagent")
	assert.Empty(t, auth.readCalls)
}

func TestNewClarifyTargetAuthorizer_JailsToTargetPackage(t *testing.T) {
	sandbox := t.TempDir()
	targetPkgDir := filepath.Join(sandbox, "targetpkg")
	require.NoError(t, os.MkdirAll(filepath.Join(targetPkgDir, "data"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(targetPkgDir, ".hidden"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(targetPkgDir, "testdata"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(targetPkgDir, "nestedpkg"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(sandbox, "otherpkg"), 0o755))

	targetFile := filepath.Join(targetPkgDir, "target.go")
	supportFile := filepath.Join(targetPkgDir, "data", "config.json")
	hiddenFile := filepath.Join(targetPkgDir, ".hidden", "config.json")
	testdataFile := filepath.Join(targetPkgDir, "testdata", "fixture.go")
	nestedPkgFile := filepath.Join(targetPkgDir, "nestedpkg", "nested.go")
	otherPkgFile := filepath.Join(sandbox, "otherpkg", "other.go")

	require.NoError(t, os.WriteFile(targetFile, []byte("package targetpkg\n"), 0o644))
	require.NoError(t, os.WriteFile(supportFile, []byte("{}"), 0o644))
	require.NoError(t, os.WriteFile(hiddenFile, []byte("{}"), 0o644))
	require.NoError(t, os.WriteFile(testdataFile, []byte("package testdata\n"), 0o644))
	require.NoError(t, os.WriteFile(nestedPkgFile, []byte("package nestedpkg\n"), 0o644))
	require.NoError(t, os.WriteFile(otherPkgFile, []byte("package otherpkg\n"), 0o644))

	auth, err := newClarifyTargetAuthorizer(authdomain.NewAutoApproveAuthorizer(sandbox), targetPkgDir)
	require.NoError(t, err)
	require.NotNil(t, auth)
	assert.True(t, auth.IsCodeUnitDomain())
	assert.Equal(t, targetPkgDir, auth.CodeUnitDir())
	assert.Equal(t, sandbox, auth.SandboxDir())

	assert.NoError(t, auth.IsAuthorizedForRead(false, "", "read_file", targetFile))
	assert.NoError(t, auth.IsAuthorizedForRead(false, "", "read_file", supportFile))
	assert.NoError(t, auth.IsAuthorizedForRead(false, "", "read_file", testdataFile))
	assert.ErrorIs(t, auth.IsAuthorizedForRead(false, "", "read_file", hiddenFile), authdomain.ErrCodeUnitPathOutside)
	assert.ErrorIs(t, auth.IsAuthorizedForRead(false, "", "read_file", nestedPkgFile), authdomain.ErrCodeUnitPathOutside)
	assert.ErrorIs(t, auth.IsAuthorizedForRead(false, "", "read_file", otherPkgFile), authdomain.ErrCodeUnitPathOutside)
}

func TestNewClarifyTargetAuthorizer_NilBaseAuthorizer(t *testing.T) {
	auth, err := newClarifyTargetAuthorizer(nil, t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, auth)
}

func TestInvokeClarifyAgent_UsesClarifyAgentAndReturnsAnswer(t *testing.T) {
	sandboxDir := t.TempDir()
	authorizer := authdomain.NewAutoApproveAuthorizer(sandboxDir)
	creator := &fakeAgentCreator{}
	invoker := &fakeAgentInvoker{
		events: successfulClarifyEvents("It compares the values using equality semantics."),
	}

	answer, err := invokeClarifyAgent(
		context.Background(),
		invoker,
		creator,
		sandboxDir,
		authorizer,
		filepath.Join(sandboxDir, "effective-sandbox"),
		authdomain.NewAutoApproveAuthorizer(filepath.Join(sandboxDir, "effective-sandbox")),
		"mock-model",
		"pkg",
		filepath.Join(sandboxDir, "pkg"),
		"Equal",
		"What does Equal do?",
	)
	require.NoError(t, err)
	assert.Equal(t, "It compares the values using equality semantics.", answer)
	assert.Equal(t, ToolNameClarifyPublicAPI, invoker.invokedAgentName)
	assert.NotNil(t, invoker.req.AgentCreator)
	assert.Equal(t, filepath.Join(sandboxDir, "pkg"), invoker.req.ToolOptions.GoPkgAbsDir)
	assert.Equal(t, llmmodel.ModelID("mock-model"), invoker.req.ToolOptions.Model)
	assert.Equal(t, filepath.Join(sandboxDir, "effective-sandbox"), invoker.req.OverrideSandboxDir)
	assert.Equal(t, sandboxDir, invoker.req.CallerSandboxDir)
	assert.Equal(t, authorizer, invoker.req.CallerAuthorizer)
	require.Len(t, invoker.req.Messages, 1)
	assert.Equal(t, "What does Equal do?", invoker.req.Messages[0])
	assert.JSONEq(t, `{"path":"pkg","identifier":"Equal","question":"What does Equal do?"}`, string(invoker.req.Payload))
}

func TestInvokeClarifyAgent_PreservesMultilineQuestionsAsPlainText(t *testing.T) {
	sandboxDir := t.TempDir()
	invoker := &fakeAgentInvoker{
		events: successfulClarifyEvents("It compares the values using equality semantics."),
	}

	_, err := invokeClarifyAgent(
		context.Background(),
		invoker,
		&fakeAgentCreator{},
		sandboxDir,
		authdomain.NewAutoApproveAuthorizer(sandboxDir),
		sandboxDir,
		authdomain.NewAutoApproveAuthorizer(sandboxDir),
		"mock-model",
		"pkg",
		filepath.Join(sandboxDir, "pkg"),
		"Equal",
		"What does \"Equal\" do?\nDoes it treat nil specially?",
	)
	require.NoError(t, err)
	require.Len(t, invoker.req.Messages, 1)
	assert.Equal(t, "What does \"Equal\" do?\nDoes it treat nil specially?", invoker.req.Messages[0])
	assert.JSONEq(t, `{"path":"pkg","identifier":"Equal","question":"What does \"Equal\" do?\nDoes it treat nil specially?"}`, string(invoker.req.Payload))
}

func TestInvokeClarifyAgent_RequiresInvoker(t *testing.T) {
	_, err := invokeClarifyAgent(
		context.Background(),
		nil,
		&fakeAgentCreator{},
		t.TempDir(),
		nil,
		t.TempDir(),
		nil,
		"",
		"fmt",
		t.TempDir(),
		"Thing",
		"What does Thing do?",
	)
	assert.EqualError(t, err, "clarify agent unavailable")
}

type fakeAgentInvoker struct {
	events           <-chan agent.Event
	err              error
	invokedAgentName string
	req              toolsetinterface.InvokeRequest
	createFn         func(context.Context, string, toolsetinterface.InvokeRequest) (*agent.Agent, error)
	invokeFn         func(context.Context, string, toolsetinterface.InvokeRequest) (<-chan agent.Event, error)
}

func (f *fakeAgentInvoker) Create(ctx context.Context, agentName string, req toolsetinterface.InvokeRequest) (*agent.Agent, error) {
	f.invokedAgentName = agentName
	f.req = req
	if f.createFn != nil {
		return f.createFn(ctx, agentName, req)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeAgentInvoker) Invoke(ctx context.Context, agentName string, req toolsetinterface.InvokeRequest) (<-chan agent.Event, error) {
	f.invokedAgentName = agentName
	f.req = req
	if f.invokeFn != nil {
		return f.invokeFn(ctx, agentName, req)
	}
	return f.events, f.err
}

type fakeAgentCreator struct {
	newFn func(string, []llmstream.Tool, ...agent.NewOptions) (*agent.Agent, error)
}

func (f *fakeAgentCreator) New(systemPrompt string, tools []llmstream.Tool, options ...agent.NewOptions) (*agent.Agent, error) {
	if f.newFn != nil {
		return f.newFn(systemPrompt, tools, options...)
	}
	return nil, errors.New("not implemented")
}

func successfulClarifyEvents(answer string) <-chan agent.Event {
	events := make(chan agent.Event, 2)
	events <- agent.Event{
		Type: agent.EventTypeAssistantTurnComplete,
		Turn: &llmstream.Turn{
			Role:  llmstream.RoleAssistant,
			Parts: []llmstream.ContentPart{llmstream.TextContent{Content: answer}},
		},
	}
	events <- agent.Event{Type: agent.EventTypeDoneSuccess}
	close(events)
	return events
}
