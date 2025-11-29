package sandboxauth

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

func TestSandboxReadInsideNoRequest(t *testing.T) {
	t.Parallel()

	commands := &ShellAllowedCommands{}
	auth, requests, err := NewSandboxAuthorizer(commands)
	require.NoError(t, err)

	sandbox := t.TempDir()
	target := filepath.Join(sandbox, "example.txt")

	err = auth.IsAuthorizedForRead(false, "", "reader", sandbox, target)
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

func TestSandboxReadRequestPermissionPrompts(t *testing.T) {
	t.Parallel()

	commands := &ShellAllowedCommands{}
	auth, requests, err := NewSandboxAuthorizer(commands)
	require.NoError(t, err)

	sandbox := t.TempDir()
	target := filepath.Join(sandbox, "notes.md")

	done := make(chan struct{})
	var callErr error

	go func() {
		callErr = auth.IsAuthorizedForRead(true, "need approval", "reader", sandbox, target)
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

	commands := &ShellAllowedCommands{}
	commands.AddDangerous(CommandMatcher{Command: "npm"})

	auth, requests, err := NewSandboxAuthorizer(commands)
	require.NoError(t, err)

	sandbox := t.TempDir()
	command := []string{"npm", "install"}

	done := make(chan struct{})
	var callErr error

	go func() {
		callErr = auth.IsShellAuthorized(false, "", sandbox, sandbox, command)
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

	auth, requests, err := NewSandboxAuthorizer(nil)
	require.NoError(t, err)

	sandbox := t.TempDir()
	command := []string{"git", "push"}

	done := make(chan struct{})
	var callErr error

	go func() {
		callErr = auth.IsShellAuthorized(false, "", sandbox, sandbox, command)
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

	commands := &ShellAllowedCommands{}
	auth, requests, err := NewPermissiveSandboxAuthorizer(commands)
	require.NoError(t, err)

	sandbox := t.TempDir()
	outsideRoot := t.TempDir()
	target := filepath.Join(outsideRoot, "data.json")

	done := make(chan struct{})
	var callErr error

	go func() {
		callErr = auth.IsAuthorizedForRead(false, "", "reader", sandbox, target)
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

	commands := &ShellAllowedCommands{}
	auth, requests, err := NewPermissiveSandboxAuthorizer(commands)
	require.NoError(t, err)

	sandbox := t.TempDir()
	command := []string{"customcmd", "--flag"}

	err = auth.IsShellAuthorized(false, "", sandbox, sandbox, command)
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

func TestPermissiveShellGitCheckoutPrompts(t *testing.T) {
	t.Parallel()

	auth, requests, err := NewPermissiveSandboxAuthorizer(nil)
	require.NoError(t, err)

	sandbox := t.TempDir()
	command := []string{"git", "checkout", "."}

	done := make(chan struct{})
	var callErr error

	go func() {
		callErr = auth.IsShellAuthorized(false, "", sandbox, sandbox, command)
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

	auth := NewAutoApproveAuthorizer()

	sandbox := t.TempDir()
	target := filepath.Join(sandbox, "output.txt")
	command := []string{"rm", "-rf", "/tmp/whatever"}

	err := auth.IsAuthorizedForRead(true, "any reason", "tool", sandbox, target)
	require.NoError(t, err)

	err = auth.IsAuthorizedForWrite(false, "", "tool", sandbox, target)
	require.NoError(t, err)

	err = auth.IsShellAuthorized(true, "delete all", sandbox, sandbox, command)
	require.NoError(t, err)

	auth.Close()
}

func TestSandboxCloseDeniesPending(t *testing.T) {
	t.Parallel()

	commands := &ShellAllowedCommands{}
	auth, requests, err := NewSandboxAuthorizer(commands)
	require.NoError(t, err)

	sandbox := t.TempDir()
	target := filepath.Join(sandbox, "secret.txt")

	done := make(chan struct{})
	var callErr error

	go func() {
		callErr = auth.IsAuthorizedForRead(true, "double check", "reader", sandbox, target)
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

func TestCodeUnitReadFileBlocksOutsideUnit(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(base, "subdir"), 0o755))
	outsidePath := filepath.Join(base, "subdir", "note.txt")
	require.NoError(t, os.WriteFile(outsidePath, []byte("hello"), 0o644))

	unit, err := codeunit.NewCodeUnit("package authorizer", base)
	require.NoError(t, err)

	fallback := &stubAuthorizer{
		readFn: func(bool, string, string, string, ...string) error {
			t.Fatal("fallback should not be invoked when path is outside code unit")
			return nil
		},
	}
	defer fallback.Close()

	auth := NewCodeUnitAuthorizer(unit, fallback)

	strictTool := strictReadToolName(t)

	err = auth.IsAuthorizedForRead(false, "", strictTool, base, outsidePath)
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
		readFn: func(requestPermission bool, requestReason string, toolName string, sandboxDir string, paths ...string) error {
			called = true
			require.True(t, requestPermission)
			require.Equal(t, "reason", requestReason)
			require.Equal(t, strictTool, toolName)
			require.Equal(t, base, sandboxDir)
			require.Equal(t, []string{allowedPath}, paths)
			return nil
		},
	}
	defer fallback.Close()

	auth := NewCodeUnitAuthorizer(unit, fallback)

	err = auth.IsAuthorizedForRead(true, "reason", strictTool, base, allowedPath)
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
		readFn: func(requestPermission bool, requestReason string, tn string, sandboxDir string, paths ...string) error {
			called = true
			require.False(t, requestPermission)
			require.Equal(t, toolName, tn)
			require.Equal(t, base, sandboxDir)
			require.Equal(t, []string{outsidePath}, paths)
			return errors.New("fallback decision")
		},
	}
	defer fallback.Close()

	auth := NewCodeUnitAuthorizer(unit, fallback)

	err = auth.IsAuthorizedForRead(false, "", toolName, base, outsidePath)
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
		writeFn: func(bool, string, string, string, ...string) error {
			t.Fatal("fallback should not be invoked when write path is outside code unit")
			return nil
		},
	}
	defer fallback.Close()

	auth := NewCodeUnitAuthorizer(unit, fallback)

	err = auth.IsAuthorizedForWrite(false, "", "write_tool", base, outsidePath)
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
		writeFn: func(requestPermission bool, requestReason string, toolName string, sandboxDir string, paths ...string) error {
			called = true
			require.True(t, requestPermission)
			require.Equal(t, "explain", requestReason)
			require.Equal(t, "write_tool", toolName)
			require.Equal(t, base, sandboxDir)
			require.Equal(t, []string{insidePath}, paths)
			return nil
		},
	}
	defer fallback.Close()

	auth := NewCodeUnitAuthorizer(unit, fallback)

	err = auth.IsAuthorizedForWrite(true, "explain", "write_tool", base, insidePath)
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
		shellFn: func(requestPermission bool, requestReason string, sandboxDir string, cwd string, command []string) error {
			called = true
			require.False(t, requestPermission)
			require.Equal(t, "shell reason", requestReason)
			require.Equal(t, base, sandboxDir)
			require.Equal(t, base, cwd)
			require.Equal(t, []string{"echo", "hi"}, command)
			return errors.New("fallback-error")
		},
	}
	defer fallback.Close()

	auth := NewCodeUnitAuthorizer(unit, fallback)

	err = auth.IsShellAuthorized(false, "shell reason", base, base, []string{"echo", "hi"})
	require.EqualError(t, err, "fallback-error")
	require.True(t, called)
}

type stubAuthorizer struct {
	readFn  func(bool, string, string, string, ...string) error
	writeFn func(bool, string, string, string, ...string) error
	shellFn func(bool, string, string, string, []string) error
	closeFn func()
}

func (s *stubAuthorizer) IsAuthorizedForRead(requestPermission bool, requestReason string, toolName string, sandboxDir string, absPath ...string) error {
	if s.readFn != nil {
		return s.readFn(requestPermission, requestReason, toolName, sandboxDir, absPath...)
	}
	return nil
}

func (s *stubAuthorizer) IsAuthorizedForWrite(requestPermission bool, requestReason string, toolName string, sandboxDir string, absPath ...string) error {
	if s.writeFn != nil {
		return s.writeFn(requestPermission, requestReason, toolName, sandboxDir, absPath...)
	}
	return nil
}

func (s *stubAuthorizer) IsShellAuthorized(requestPermission bool, requestReason string, sandboxDir string, cwd string, command []string) error {
	if s.shellFn != nil {
		return s.shellFn(requestPermission, requestReason, sandboxDir, cwd, command)
	}
	return nil
}

func (s *stubAuthorizer) Close() {
	if s.closeFn != nil {
		s.closeFn()
	}
}
