package authdomain

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/stretchr/testify/require"
)

func TestWithUpdatedSandboxRejectsNil(t *testing.T) {
	t.Parallel()

	updated, err := WithUpdatedSandbox(nil, t.TempDir())
	require.Error(t, err)
	require.Nil(t, updated)
}

func TestWithUpdatedSandboxRejectsEmptySandbox(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	auth := NewAutoApproveAuthorizer(sandbox)

	updated, err := WithUpdatedSandbox(auth, "")
	require.Error(t, err)
	require.Nil(t, updated)
}

func TestWithUpdatedSandboxSandboxAuthorizerSharesRequestsAndGrants(t *testing.T) {
	t.Parallel()

	sandbox1 := t.TempDir()
	sandbox2 := t.TempDir()

	auth1, requests, err := NewSandboxAuthorizer(sandbox1, nil)
	require.NoError(t, err)
	defer auth1.Close()

	updated, err := WithUpdatedSandbox(auth1, sandbox2)
	require.NoError(t, err)
	require.Equal(t, filepath.Clean(sandbox1), auth1.SandboxDir())
	require.Equal(t, filepath.Clean(sandbox2), updated.SandboxDir())

	// Verify grants are shared by adding a grant after WithUpdatedSandbox and ensuring it applies to the updated authorizer.
	grantTarget := filepath.Join(sandbox2, "example.txt")
	require.NoError(t, os.WriteFile(grantTarget, []byte("hello"), 0o644))
	require.NoError(t, AddGrantsFromUserMessage(auth1, "please read @example.txt"))

	done := make(chan error, 1)
	go func() {
		done <- updated.IsAuthorizedForRead(true, "", "read_file", grantTarget)
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case req := <-requests:
		t.Fatalf("unexpected prompt: %#v", req)
	case <-time.After(time.Second):
		t.Fatal("authorization call blocked unexpectedly")
	}

	// Verify the request channel is shared by triggering a prompt from the updated authorizer.
	needPrompt := filepath.Join(sandbox2, "needs_prompt.txt")
	require.NoError(t, os.WriteFile(needPrompt, []byte("x"), 0o644))

	done = make(chan error, 1)
	go func() {
		done <- updated.IsAuthorizedForRead(true, "reason", "reader", needPrompt)
	}()

	var req UserRequest
	select {
	case req = <-requests:
	case <-time.After(time.Second):
		t.Fatal("expected prompt on shared requests channel")
	}
	require.Equal(t, "reader", req.ToolName)
	require.Contains(t, req.Prompt, needPrompt)
	req.Allow()

	require.NoError(t, <-done)
}

func TestWithUpdatedSandboxPermissiveSandboxAuthorizerUsesUpdatedRoot(t *testing.T) {
	t.Parallel()

	sandbox1 := t.TempDir()
	sandbox2 := t.TempDir()

	auth1, requests, err := NewPermissiveSandboxAuthorizer(sandbox1, nil)
	require.NoError(t, err)
	defer auth1.Close()

	updated, err := WithUpdatedSandbox(auth1, sandbox2)
	require.NoError(t, err)

	// A path inside sandbox1 becomes outside sandbox2, so permissive policy should prompt.
	target := filepath.Join(sandbox1, "data.json")
	require.NoError(t, os.WriteFile(target, []byte("{}"), 0o644))

	done := make(chan error, 1)
	go func() {
		done <- updated.IsAuthorizedForRead(false, "", "reader", target)
	}()

	var req UserRequest
	select {
	case req = <-requests:
	case <-time.After(time.Second):
		t.Fatal("expected prompt for outside-sandbox read")
	}
	require.Contains(t, req.Prompt, "outside the sandbox")
	req.Disallow()

	require.ErrorIs(t, <-done, ErrAuthorizationDenied)
}

func TestWithUpdatedSandboxCodeUnitUpdatesFallbackSandboxAndSharesGrants(t *testing.T) {
	t.Parallel()

	sandbox1 := t.TempDir()
	sandbox2 := t.TempDir()

	unitDir := filepath.Join(sandbox1, "unit")
	require.NoError(t, os.MkdirAll(unitDir, 0o755))
	unit, err := codeunit.NewCodeUnit("unit", unitDir)
	require.NoError(t, err)

	fallback, requests, err := NewSandboxAuthorizer(sandbox1, nil)
	require.NoError(t, err)

	auth := NewCodeUnitAuthorizer(unit, fallback)
	defer auth.Close()

	updated, err := WithUpdatedSandbox(auth, sandbox2)
	require.NoError(t, err)
	require.True(t, updated.IsCodeUnitDomain())
	require.Equal(t, filepath.Clean(sandbox2), updated.SandboxDir())

	// Shell should be authorized in the updated sandbox (and denied by the original due to cwd outside sandbox1).
	require.Error(t, auth.IsShellAuthorized(false, "", sandbox2, []string{"ls"}))
	require.NoError(t, updated.IsShellAuthorized(false, "", sandbox2, []string{"ls"}))

	// Verify the fallback request channel is shared by causing a dangerous command prompt from the updated authorizer.
	done := make(chan error, 1)
	go func() {
		done <- updated.IsShellAuthorized(false, "", sandbox2, []string{"git", "push"})
	}()

	var req UserRequest
	select {
	case req = <-requests:
	case <-time.After(time.Second):
		t.Fatal("expected prompt on shared requests channel")
	}
	require.Equal(t, []string{"git", "push"}, req.Argv)
	req.Disallow()
	require.ErrorIs(t, <-done, ErrAuthorizationDenied)

	// Verify code-unit grants are shared by adding a grant after WithUpdatedSandbox and ensuring the updated authorizer honors it.
	readme := filepath.Join(sandbox2, "README.md")
	require.NoError(t, os.WriteFile(readme, []byte("hi"), 0o644))
	require.NoError(t, AddGrantsFromUserMessage(auth, "Read @README.md"))
	require.NoError(t, updated.IsAuthorizedForRead(false, "", "read_file", readme))
}

func TestWithUpdatedSandboxRejectsUnknownAuthorizerType(t *testing.T) {
	t.Parallel()

	auth := &stubAuthorizer{sandboxDir: t.TempDir()}
	updated, err := WithUpdatedSandbox(auth, t.TempDir())
	require.Error(t, err)
	require.Nil(t, updated)
}
