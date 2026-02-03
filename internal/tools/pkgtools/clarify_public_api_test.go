package pkgtools

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/stretchr/testify/assert"
)

type denyReadAuthorizer struct {
	sandboxDir string
	readCalls  []string
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
