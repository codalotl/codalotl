package authdomain

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/stretchr/testify/require"
)

func TestAddGrantsFromUserMessageRejectsNonGrantCapableAuthorizer(t *testing.T) {
	t.Parallel()

	auth := &stubAuthorizer{sandboxDir: t.TempDir()}
	err := AddGrantsFromUserMessage(auth, "read @README.md")
	require.ErrorIs(t, err, ErrAuthorizerCannotAcceptGrants)
}

func TestSandboxReadFileGrantSkipsRequestPermissionPrompt(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	auth, requests, err := NewSandboxAuthorizer(sandbox, nil)
	require.NoError(t, err)
	defer auth.Close()

	target := filepath.Join(sandbox, "example.txt")
	require.NoError(t, os.WriteFile(target, []byte("hello"), 0o644))

	require.NoError(t, AddGrantsFromUserMessage(auth, "please read @example.txt."))

	done := make(chan error, 1)
	go func() {
		done <- auth.IsAuthorizedForRead(true, "explicit", "read_file", target)
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case req := <-requests:
		t.Fatalf("unexpected prompt: %#v", req)
	case <-time.After(time.Second):
		t.Fatal("authorization call blocked unexpectedly")
	}
}

func TestSandboxReadFileGrantDoesNotAuthorizeOutsideSandbox(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	auth, requests, err := NewSandboxAuthorizer(sandbox, nil)
	require.NoError(t, err)
	defer auth.Close()

	outsideRoot := t.TempDir()
	target := filepath.Join(outsideRoot, "outside.txt")
	require.NoError(t, os.WriteFile(target, []byte("hello"), 0o644))

	require.NoError(t, AddGrantsFromUserMessage(auth, "read @"+target))
	err = auth.IsAuthorizedForRead(false, "", "read_file", target)
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside sandbox")

	select {
	case req := <-requests:
		t.Fatalf("unexpected prompt: %#v", req)
	default:
	}
}

func TestPermissiveReadFileGrantAuthorizesOutsideSandboxWithoutPrompt(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	auth, requests, err := NewPermissiveSandboxAuthorizer(sandbox, nil)
	require.NoError(t, err)
	defer auth.Close()

	outsideRoot := t.TempDir()
	target := filepath.Join(outsideRoot, "outside.txt")
	require.NoError(t, os.WriteFile(target, []byte("hello"), 0o644))

	require.NoError(t, AddGrantsFromUserMessage(auth, "read @"+target))

	done := make(chan error, 1)
	go func() {
		done <- auth.IsAuthorizedForRead(false, "", "read_file", target)
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case req := <-requests:
		t.Fatalf("unexpected prompt: %#v", req)
	case <-time.After(time.Second):
		t.Fatal("authorization call blocked unexpectedly")
	}
}

func TestCodeUnitGrantsReadOnlyToolsTableDriven(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	unitDir := filepath.Join(sandbox, "unit")
	require.NoError(t, os.MkdirAll(unitDir, 0o755))

	outsideRel := "README.md"
	outsideAbs := filepath.Join(sandbox, outsideRel)
	require.NoError(t, os.WriteFile(outsideAbs, []byte("hello"), 0o644))

	spaceRel := "my file.txt"
	spaceAbs := filepath.Join(sandbox, spaceRel)
	require.NoError(t, os.WriteFile(spaceAbs, []byte("hi"), 0o644))

	unit, err := codeunit.NewCodeUnit("package unit", unitDir)
	require.NoError(t, err)

	cases := []struct {
		name     string
		toolName string
		path     string
		wantErr  bool
	}{
		{name: "ReadFileAllowed", toolName: "read_file", path: outsideAbs, wantErr: false},
		{name: "LsAllowed", toolName: "ls", path: outsideAbs, wantErr: false},
		{name: "DiagnosticsStillBlocked", toolName: "diagnostics", path: outsideAbs, wantErr: true},
		{name: "RunTestsStillBlocked", toolName: "run_tests", path: outsideAbs, wantErr: true},
		{name: "QuotedSpacesAllowed", toolName: "read_file", path: spaceAbs, wantErr: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fallback, requests, err := NewPermissiveSandboxAuthorizer(sandbox, nil)
			require.NoError(t, err)

			auth := NewCodeUnitAuthorizer(unit, fallback)
			defer auth.Close()

			require.NoError(t, AddGrantsFromUserMessage(auth, `Read @README.md. Also read @"my file.txt".`))

			done := make(chan error, 1)
			go func() {
				done <- auth.IsAuthorizedForRead(true, "explicit", tc.toolName, tc.path)
			}()

			select {
			case err := <-done:
				if tc.wantErr {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
			case req := <-requests:
				t.Fatalf("unexpected prompt for %s: %#v", tc.name, req)
			case <-time.After(time.Second):
				t.Fatalf("authorization call blocked for %s", tc.name)
			}
		})
	}
}

func TestCodeUnitDirectoryAndGlobGrantsTableDriven(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	unitDir := filepath.Join(sandbox, "unit")
	require.NoError(t, os.MkdirAll(unitDir, 0o755))

	require.NoError(t, os.MkdirAll(filepath.Join(sandbox, "docs", "nested"), 0o755))
	topFile := filepath.Join(sandbox, "docs", "top.md")
	nestedFile := filepath.Join(sandbox, "docs", "nested", "child.md")
	require.NoError(t, os.WriteFile(topFile, []byte("top"), 0o644))
	require.NoError(t, os.WriteFile(nestedFile, []byte("child"), 0o644))

	unit, err := codeunit.NewCodeUnit("package unit", unitDir)
	require.NoError(t, err)

	cases := []struct {
		name      string
		userMsg   string
		path      string
		wantGrant bool
	}{
		{name: "DirectoryRecursive", userMsg: "read @docs", path: nestedFile, wantGrant: true},
		{name: "DirectoryNonRecursiveGlob", userMsg: "read @docs/*", path: nestedFile, wantGrant: false},
		{name: "DirectoryNonRecursiveGlobTop", userMsg: "read @docs/*", path: topFile, wantGrant: true},
		{name: "GlobFiltersByExtension", userMsg: "read @docs/*.md", path: topFile, wantGrant: true},
		{name: "GlobDoesNotMatchNested", userMsg: "read @docs/*.md", path: nestedFile, wantGrant: false},
		{name: "RootCannotBeGranted", userMsg: "read @/", path: topFile, wantGrant: false},
		{name: "InvalidGlobDoesNotGrant", userMsg: "read @[abc", path: topFile, wantGrant: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			auth := NewCodeUnitAuthorizer(unit, NewAutoApproveAuthorizer(sandbox))
			defer auth.Close()

			require.NoError(t, AddGrantsFromUserMessage(auth, tc.userMsg))
			err := auth.IsAuthorizedForRead(false, "", "read_file", tc.path)
			if tc.wantGrant {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), "outside")
		})
	}
}
