package authdomain

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/codalotl/codalotl/internal/codeunit"
)

func strictReadToolName(t *testing.T) string {
	if len(codeUnitStrictReadToolNames) == 0 {
		t.Fatal("codeUnitStrictReadToolNames is empty")
	}
	return codeUnitStrictReadToolNames[0]
}

func TestSandboxAuthorizerDomainMetadata(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	sandboxArg := filepath.Join(base, "child", "..")
	commands := &ShellAllowedCommands{}

	auth, requests, err := NewSandboxAuthorizer(sandboxArg, commands)
	require.NoError(t, err)
	require.NotNil(t, requests)

	require.Equal(t, filepath.Clean(base), auth.SandboxDir())
	require.Empty(t, auth.CodeUnitDir())
	require.False(t, auth.IsCodeUnitDomain())
	require.True(t, auth == auth.WithoutCodeUnit())

	auth.Close()
	_, ok := <-requests
	require.False(t, ok)
}

func TestNewSandboxAuthorizerRejectsEmptySandbox(t *testing.T) {
	t.Parallel()

	auth, requests, err := NewSandboxAuthorizer("", nil)
	require.Error(t, err)
	require.Nil(t, auth)
	require.Nil(t, requests)
}

func TestNewPermissiveSandboxAuthorizerRejectsEmptySandbox(t *testing.T) {
	t.Parallel()

	auth, requests, err := NewPermissiveSandboxAuthorizer("", nil)
	require.Error(t, err)
	require.Nil(t, auth)
	require.Nil(t, requests)
}

func TestSandboxReadInsideNoRequest(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	commands := &ShellAllowedCommands{}
	auth, requests, err := NewSandboxAuthorizer(sandbox, commands)
	require.NoError(t, err)

	target := filepath.Join(sandbox, "example.txt")

	err = auth.IsAuthorizedForRead(false, "", "reader", target)
	require.NoError(t, err)

	select {
	case req, ok := <-requests:
		if ok {
			t.Fatalf("unexpected request: %#v", req)
		}
	default:
	}

	auth.Close()
	_, ok := <-requests
	require.False(t, ok)
}

func TestSandboxReadOutsideDenied(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	commands := &ShellAllowedCommands{}
	auth, requests, err := NewSandboxAuthorizer(sandbox, commands)
	require.NoError(t, err)

	outside := filepath.Join(t.TempDir(), "outside.txt")
	err = auth.IsAuthorizedForRead(false, "", "reader", outside)
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside sandbox")

	select {
	case req, ok := <-requests:
		if ok {
			t.Fatalf("unexpected request: %#v", req)
		}
	default:
	}

	auth.Close()
	_, ok := <-requests
	require.False(t, ok)
}

func TestSandboxReadEmptyPathReturnsError(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	auth, requests, err := NewSandboxAuthorizer(sandbox, nil)
	require.NoError(t, err)

	err = auth.IsAuthorizedForRead(false, "", "reader", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "path is empty")

	auth.Close()
	_, ok := <-requests
	require.False(t, ok)
}

func TestSandboxReadRequestPermissionPrompts(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	commands := &ShellAllowedCommands{}
	auth, requests, err := NewSandboxAuthorizer(sandbox, commands)
	require.NoError(t, err)

	target := filepath.Join(sandbox, "notes.md")

	done := make(chan struct{})
	var callErr error

	go func() {
		callErr = auth.IsAuthorizedForRead(true, "need approval", "reader", target)
		close(done)
	}()

	req := <-requests
	require.Equal(t, "reader", req.ToolName)
	require.Nil(t, req.Argv)
	require.Contains(t, req.Prompt, "tool \"reader\" to read")
	require.Contains(t, req.Prompt, "inside the sandbox")
	require.Contains(t, req.Prompt, "explicit permission requested")
	require.Contains(t, req.Prompt, target)
	require.Contains(t, req.Prompt, "Reason: need approval")

	req.Allow()
	<-done

	require.NoError(t, callErr)

	auth.Close()
	_, ok := <-requests
	require.False(t, ok)
}

func TestSandboxShellDangerousRequests(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	commands := &ShellAllowedCommands{}
	commands.AddDangerous(CommandMatcher{Command: "npm"})

	auth, requests, err := NewSandboxAuthorizer(sandbox, commands)
	require.NoError(t, err)

	command := []string{"npm", "install"}

	done := make(chan struct{})
	var callErr error

	go func() {
		callErr = auth.IsShellAuthorized(false, "", sandbox, command)
		close(done)
	}()

	req := <-requests
	require.Empty(t, req.ToolName)
	require.Equal(t, command, req.Argv)
	require.Contains(t, req.Prompt, "dangerous)")
	require.Contains(t, req.Prompt, strings.Join(command, " "))

	req.Disallow()
	<-done

	require.ErrorIs(t, callErr, ErrAuthorizationDenied)

	auth.Close()
	_, ok := <-requests
	require.False(t, ok)
}

func TestSandboxShellUsesDefaultCommandsWhenNil(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	auth, requests, err := NewSandboxAuthorizer(sandbox, nil)
	require.NoError(t, err)

	command := []string{"git", "push"}

	done := make(chan struct{})
	var callErr error

	go func() {
		callErr = auth.IsShellAuthorized(false, "", sandbox, command)
		close(done)
	}()

	req := <-requests
	require.Empty(t, req.ToolName)
	require.Equal(t, command, req.Argv)
	require.Contains(t, req.Prompt, "dangerous)")
	require.Contains(t, req.Prompt, strings.Join(command, " "))

	req.Disallow()
	<-done

	require.ErrorIs(t, callErr, ErrAuthorizationDenied)

	auth.Close()
	_, ok := <-requests
	require.False(t, ok)
}

func TestPermissiveReadOutsideRequests(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	commands := &ShellAllowedCommands{}
	auth, requests, err := NewPermissiveSandboxAuthorizer(sandbox, commands)
	require.NoError(t, err)

	outsideRoot := t.TempDir()
	target := filepath.Join(outsideRoot, "data.json")

	done := make(chan struct{})
	var callErr error

	go func() {
		callErr = auth.IsAuthorizedForRead(false, "", "reader", target)
		close(done)
	}()

	req := <-requests
	require.Equal(t, "reader", req.ToolName)
	require.Nil(t, req.Argv)
	require.Contains(t, req.Prompt, "outside the sandbox")
	require.Contains(t, req.Prompt, target)

	req.Allow()
	<-done

	require.NoError(t, callErr)

	auth.Close()
	_, ok := <-requests
	require.False(t, ok)
}

func TestPermissiveShellNoneAllows(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	commands := &ShellAllowedCommands{}
	auth, requests, err := NewPermissiveSandboxAuthorizer(sandbox, commands)
	require.NoError(t, err)

	command := []string{"customcmd", "--flag"}

	err = auth.IsShellAuthorized(false, "", sandbox, command)
	require.NoError(t, err)

	select {
	case req, ok := <-requests:
		if ok {
			t.Fatalf("unexpected request: %#v", req)
		}
	default:
	}

	auth.Close()
	_, ok := <-requests
	require.False(t, ok)
}

func TestPermissiveShellCwdOutsidePrompts(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	outside := t.TempDir()
	auth, requests, err := NewPermissiveSandboxAuthorizer(sandbox, nil)
	require.NoError(t, err)

	command := []string{"ls"}

	done := make(chan struct{})
	var callErr error

	go func() {
		callErr = auth.IsShellAuthorized(false, "", outside, command)
		close(done)
	}()

	req := <-requests
	require.Equal(t, command, req.Argv)
	require.Contains(t, req.Prompt, "safe")
	require.Contains(t, req.Prompt, "cwd outside sandbox")

	req.Disallow()
	<-done

	require.ErrorIs(t, callErr, ErrAuthorizationDenied)

	auth.Close()
	_, ok := <-requests
	require.False(t, ok)
}

func TestPermissiveShellGitCheckoutPrompts(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	auth, requests, err := NewPermissiveSandboxAuthorizer(sandbox, nil)
	require.NoError(t, err)

	command := []string{"git", "checkout", "."}

	done := make(chan struct{})
	var callErr error

	go func() {
		callErr = auth.IsShellAuthorized(false, "", sandbox, command)
		close(done)
	}()

	var req UserRequest
	select {
	case req = <-requests:
	case <-time.After(time.Second):
		t.Fatal("expected authorization prompt for git checkout")
	}
	require.Empty(t, req.ToolName)
	require.Equal(t, command, req.Argv)
	require.Contains(t, req.Prompt, "dangerous)")
	require.Contains(t, req.Prompt, strings.Join(command, " "))

	req.Disallow()
	<-done

	require.ErrorIs(t, callErr, ErrAuthorizationDenied)

	auth.Close()
	_, ok := <-requests
	require.False(t, ok)
}

func TestAutoApproveAlwaysAllow(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	auth := NewAutoApproveAuthorizer(sandbox)

	target := filepath.Join(sandbox, "output.txt")
	command := []string{"rm", "-rf", "/tmp/whatever"}

	err := auth.IsAuthorizedForRead(true, "any reason", "tool", target)
	require.NoError(t, err)

	err = auth.IsAuthorizedForWrite(false, "", "tool", target)
	require.NoError(t, err)

	err = auth.IsShellAuthorized(true, "delete all", sandbox, command)
	require.NoError(t, err)

	auth.Close()
}

func TestNewAutoApproveAuthorizerPanicsOnInvalidSandbox(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		_ = NewAutoApproveAuthorizer("")
	})
}

func TestSandboxCloseDeniesPending(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	commands := &ShellAllowedCommands{}
	auth, requests, err := NewSandboxAuthorizer(sandbox, commands)
	require.NoError(t, err)

	target := filepath.Join(sandbox, "secret.txt")

	done := make(chan struct{})
	var callErr error

	go func() {
		callErr = auth.IsAuthorizedForRead(true, "double check", "reader", target)
		close(done)
	}()

	req := <-requests
	require.Equal(t, "reader", req.ToolName)
	require.Nil(t, req.Argv)
	require.NotNil(t, req.Allow)
	require.NotNil(t, req.Disallow)

	auth.Close()
	<-done

	require.True(t, errors.Is(callErr, ErrAuthorizerClosed))

	_, ok := <-requests
	require.False(t, ok)
}

func TestSandboxClosedAuthorizerRejectsCalls(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	auth, requests, err := NewSandboxAuthorizer(sandbox, nil)
	require.NoError(t, err)

	auth.Close()
	_, ok := <-requests
	require.False(t, ok)

	err = auth.IsAuthorizedForRead(false, "", "reader", filepath.Join(sandbox, "file.txt"))
	require.ErrorIs(t, err, ErrAuthorizerClosed)
}

func TestNewCodeUnitAuthorizerPanicsOnNilInputs(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	unit, err := codeunit.NewCodeUnit("package authorizer", sandbox)
	require.NoError(t, err)

	require.Panics(t, func() {
		_ = NewCodeUnitAuthorizer(nil, &stubAuthorizer{sandboxDir: sandbox})
	})

	require.Panics(t, func() {
		_ = NewCodeUnitAuthorizer(unit, nil)
	})
}

func TestCodeUnitAuthorizerDomainMetadata(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	unit, err := codeunit.NewCodeUnit("package authorizer", base)
	require.NoError(t, err)

	fallback := &stubAuthorizer{sandboxDir: base}
	auth := NewCodeUnitAuthorizer(unit, fallback)

	require.Equal(t, base, auth.SandboxDir())
	require.Equal(t, base, auth.CodeUnitDir())
	require.True(t, auth.IsCodeUnitDomain())
	require.True(t, auth.WithoutCodeUnit() == fallback)
}

func TestCodeUnitCloseDelegatesToFallback(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	unit, err := codeunit.NewCodeUnit("package authorizer", base)
	require.NoError(t, err)

	closed := false
	fallback := &stubAuthorizer{
		sandboxDir: base,
		closeFn: func() {
			closed = true
		},
	}

	auth := NewCodeUnitAuthorizer(unit, fallback)
	auth.Close()

	require.True(t, closed)
}

func TestCodeUnitReadFileBlocksOutsideUnit(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(base, "subdir"), 0o755))
	outsidePath := filepath.Join(base, "subdir", "note.txt")
	require.NoError(t, os.WriteFile(outsidePath, []byte("hello"), 0o644))

	unit, err := codeunit.NewCodeUnit("package authorizer", base)
	require.NoError(t, err)

	fallback := &stubAuthorizer{
		sandboxDir: base,
		readFn: func(bool, string, string, ...string) error {
			t.Fatal("fallback should not be invoked when path is outside code unit")
			return nil
		},
	}
	defer fallback.Close()

	auth := NewCodeUnitAuthorizer(unit, fallback)

	strictTool := strictReadToolName(t)

	err = auth.IsAuthorizedForRead(false, "", strictTool, outsidePath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "is outside")
}

func TestCodeUnitReadFileDelegatesToFallback(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	allowedPath := filepath.Join(base, "allowed.txt")
	require.NoError(t, os.WriteFile(allowedPath, []byte("content"), 0o644))

	unit, err := codeunit.NewCodeUnit("package authorizer", base)
	require.NoError(t, err)

	strictTool := strictReadToolName(t)

	var called bool
	fallback := &stubAuthorizer{
		sandboxDir: base,
		readFn: func(requestPermission bool, requestReason string, toolName string, paths ...string) error {
			called = true
			require.True(t, requestPermission)
			require.Equal(t, "reason", requestReason)
			require.Equal(t, strictTool, toolName)
			require.Equal(t, []string{allowedPath}, paths)
			return nil
		},
	}
	defer fallback.Close()

	auth := NewCodeUnitAuthorizer(unit, fallback)

	err = auth.IsAuthorizedForRead(true, "reason", strictTool, allowedPath)
	require.NoError(t, err)
	require.True(t, called)
}

func TestCodeUnitOtherToolReadDelegatesOutsideUnit(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(base, "nested"), 0o755))
	outsidePath := filepath.Join(base, "nested", "note.txt")
	require.NoError(t, os.WriteFile(outsidePath, []byte("content"), 0o644))

	unit, err := codeunit.NewCodeUnit("package authorizer", base)
	require.NoError(t, err)

	var called bool
	const toolName = "search_files"
	fallback := &stubAuthorizer{
		sandboxDir: base,
		readFn: func(requestPermission bool, requestReason string, tn string, paths ...string) error {
			called = true
			require.False(t, requestPermission)
			require.Equal(t, toolName, tn)
			require.Equal(t, []string{outsidePath}, paths)
			return errors.New("fallback decision")
		},
	}
	defer fallback.Close()

	auth := NewCodeUnitAuthorizer(unit, fallback)

	err = auth.IsAuthorizedForRead(false, "", toolName, outsidePath)
	require.EqualError(t, err, "fallback decision")
	require.True(t, called)
}

func TestCodeUnitWriteBlocksOutsideUnit(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(base, "tmp"), 0o755))
	outsidePath := filepath.Join(base, "tmp", "data.txt")

	unit, err := codeunit.NewCodeUnit("package authorizer", base)
	require.NoError(t, err)

	fallback := &stubAuthorizer{
		sandboxDir: base,
		writeFn: func(bool, string, string, ...string) error {
			t.Fatal("fallback should not be invoked when write path is outside code unit")
			return nil
		},
	}
	defer fallback.Close()

	auth := NewCodeUnitAuthorizer(unit, fallback)

	err = auth.IsAuthorizedForWrite(false, "", "write_tool", outsidePath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "is outside")
}

func TestCodeUnitWriteDelegatesToFallback(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	insidePath := filepath.Join(base, "output.txt")
	require.NoError(t, os.WriteFile(insidePath, []byte("existing"), 0o644))

	unit, err := codeunit.NewCodeUnit("package authorizer", base)
	require.NoError(t, err)

	var called bool
	fallback := &stubAuthorizer{
		sandboxDir: base,
		writeFn: func(requestPermission bool, requestReason string, toolName string, paths ...string) error {
			called = true
			require.True(t, requestPermission)
			require.Equal(t, "explain", requestReason)
			require.Equal(t, "write_tool", toolName)
			require.Equal(t, []string{insidePath}, paths)
			return nil
		},
	}
	defer fallback.Close()

	auth := NewCodeUnitAuthorizer(unit, fallback)

	err = auth.IsAuthorizedForWrite(true, "explain", "write_tool", insidePath)
	require.NoError(t, err)
	require.True(t, called)
}

func TestCodeUnitShellDelegates(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	unit, err := codeunit.NewCodeUnit("package authorizer", base)
	require.NoError(t, err)

	var called bool
	fallback := &stubAuthorizer{
		sandboxDir: base,
		shellFn: func(requestPermission bool, requestReason string, cwd string, command []string) error {
			called = true
			require.False(t, requestPermission)
			require.Equal(t, "shell reason", requestReason)
			require.Equal(t, base, cwd)
			require.Equal(t, []string{"echo", "hi"}, command)
			return errors.New("fallback-error")
		},
	}
	defer fallback.Close()

	auth := NewCodeUnitAuthorizer(unit, fallback)

	err = auth.IsShellAuthorized(false, "shell reason", base, []string{"echo", "hi"})
	require.EqualError(t, err, "fallback-error")
	require.True(t, called)
}

type stubAuthorizer struct {
	sandboxDir string
	readFn     func(bool, string, string, ...string) error
	writeFn    func(bool, string, string, ...string) error
	shellFn    func(bool, string, string, []string) error
	closeFn    func()
}

func (s *stubAuthorizer) SandboxDir() string {
	return s.sandboxDir
}

func (s *stubAuthorizer) CodeUnitDir() string {
	return ""
}

func (s *stubAuthorizer) IsCodeUnitDomain() bool {
	return false
}

func (s *stubAuthorizer) WithoutCodeUnit() Authorizer {
	return s
}

func (s *stubAuthorizer) IsAuthorizedForRead(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	if s.readFn != nil {
		return s.readFn(requestPermission, requestReason, toolName, absPath...)
	}
	return nil
}

func (s *stubAuthorizer) IsAuthorizedForWrite(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	if s.writeFn != nil {
		return s.writeFn(requestPermission, requestReason, toolName, absPath...)
	}
	return nil
}

func (s *stubAuthorizer) IsShellAuthorized(requestPermission bool, requestReason string, cwd string, command []string) error {
	if s.shellFn != nil {
		return s.shellFn(requestPermission, requestReason, cwd, command)
	}
	return nil
}

func (s *stubAuthorizer) Close() {
	if s.closeFn != nil {
		s.closeFn()
	}
}
