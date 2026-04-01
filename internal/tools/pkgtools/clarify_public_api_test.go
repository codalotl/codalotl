package pkgtools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestNewClarifyTargetAuthorizer_JailsToTargetPackage(t *testing.T) {
	sandbox := t.TempDir()
	targetPkgDir := filepath.Join(sandbox, "targetpkg")
	require.NoError(t, os.MkdirAll(filepath.Join(targetPkgDir, "data"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(targetPkgDir, "testdata"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(targetPkgDir, "nestedpkg"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(sandbox, "otherpkg"), 0o755))

	targetFile := filepath.Join(targetPkgDir, "target.go")
	supportFile := filepath.Join(targetPkgDir, "data", "config.json")
	testdataFile := filepath.Join(targetPkgDir, "testdata", "fixture.go")
	nestedPkgFile := filepath.Join(targetPkgDir, "nestedpkg", "nested.go")
	otherPkgFile := filepath.Join(sandbox, "otherpkg", "other.go")

	require.NoError(t, os.WriteFile(targetFile, []byte("package targetpkg\n"), 0o644))
	require.NoError(t, os.WriteFile(supportFile, []byte("{}"), 0o644))
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
	assert.ErrorIs(t, auth.IsAuthorizedForRead(false, "", "read_file", nestedPkgFile), authdomain.ErrCodeUnitPathOutside)
	assert.ErrorIs(t, auth.IsAuthorizedForRead(false, "", "read_file", otherPkgFile), authdomain.ErrCodeUnitPathOutside)
}

func TestNewClarifyTargetAuthorizer_NilBaseAuthorizer(t *testing.T) {
	auth, err := newClarifyTargetAuthorizer(nil, t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, auth)
}
