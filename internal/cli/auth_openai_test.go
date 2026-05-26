package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/subscriptions/openaisub"
	"github.com/stretchr/testify/require"
)

func restoreOpenAIAuthStubs(t *testing.T) {
	t.Helper()

	origLogin := runOpenAISubLogin
	origLogout := runOpenAISubLogout
	origCheckStatus := runOpenAISubCheckStatus
	t.Cleanup(func() {
		runOpenAISubLogin = origLogin
		runOpenAISubLogout = origLogout
		runOpenAISubCheckStatus = origCheckStatus
	})
}

func TestRun_AuthOpenAILogin_DoesNotRequireStartupValidation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		args          []string
		wantNoBrowser bool
	}{
		{
			name:          "browser allowed",
			args:          []string{"codalotl", "auth", "openai", "login"},
			wantNoBrowser: false,
		},
		{
			name:          "no browser",
			args:          []string{"codalotl", "auth", "openai", "login", "--no-browser"},
			wantNoBrowser: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfig(t)
			restoreOpenAIAuthStubs(t)
			t.Setenv("OPENAI_API_KEY", "")
			t.Setenv("PATH", "")

			called := false
			runOpenAISubLogin = func(ctx context.Context, opts openaisub.LoginOptions) error {
				called = true
				require.NotNil(t, ctx)
				require.Equal(t, tc.wantNoBrowser, opts.NoBrowser)
				_, err := opts.Out.Write([]byte("subscription login flow\n"))
				require.NoError(t, err)
				return nil
			}

			var out bytes.Buffer
			var errOut bytes.Buffer
			code, err := Run(tc.args, &RunOptions{Out: &out, Err: &errOut})
			require.NoError(t, err)
			require.Equal(t, 0, code)
			require.True(t, called)
			require.Empty(t, strings.TrimSpace(errOut.String()))
			require.Contains(t, out.String(), "subscription login flow")
			require.Contains(t, out.String(), "login complete")
		})
	}
}

func TestRun_AuthOpenAILogout_DoesNotRequireStartupValidation(t *testing.T) {
	isolateUserConfig(t)
	restoreOpenAIAuthStubs(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("PATH", "")

	called := false
	runOpenAISubLogout = func(opts openaisub.Options) error {
		called = true
		_, err := opts.Out.Write([]byte("logout cleanup\n"))
		require.NoError(t, err)
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "auth", "openai", "logout"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.True(t, called)
	require.Empty(t, strings.TrimSpace(errOut.String()))
	require.Contains(t, out.String(), "logout cleanup")
	require.Contains(t, out.String(), "credentials removed")
}

func TestRun_AuthOpenAIStatus_LoggedIn(t *testing.T) {
	isolateUserConfig(t)
	restoreOpenAIAuthStubs(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("PATH", "")

	expiresAt := time.Date(2026, 5, 25, 10, 30, 0, 0, time.FixedZone("Test", -7*60*60))
	runOpenAISubCheckStatus = func(ctx context.Context, opts openaisub.Options) (openaisub.Status, error) {
		require.NotNil(t, ctx)
		return openaisub.Status{
			LoggedIn:         true,
			Path:             "/tmp/openai_auth.json",
			ChatGPTAccountID: "acct_123",
			ExpiresAt:        expiresAt,
		}, nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "auth", "openai", "status"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, strings.TrimSpace(errOut.String()))

	got := out.String()
	require.Contains(t, got, "logged in")
	require.Contains(t, got, "acct_123")
	require.Contains(t, got, "2026-05-25T17:30:00Z")
	require.Contains(t, got, "/tmp/openai_auth.json")
}

func TestRun_AuthOpenAIStatus_NotLoggedInUsesExitCode1(t *testing.T) {
	isolateUserConfig(t)
	restoreOpenAIAuthStubs(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("PATH", "")

	runOpenAISubCheckStatus = func(ctx context.Context, opts openaisub.Options) (openaisub.Status, error) {
		return openaisub.Status{
			LoggedIn: false,
			Path:     "/tmp/openai_auth.json",
		}, nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "auth", "openai", "status"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 1, code)
	require.Empty(t, strings.TrimSpace(errOut.String()))

	got := out.String()
	require.Contains(t, got, "not logged in")
	require.Contains(t, got, "/tmp/openai_auth.json")
}

func TestRun_AuthOpenAIStatus_ErrorUsesExitCode1(t *testing.T) {
	isolateUserConfig(t)
	restoreOpenAIAuthStubs(t)

	runOpenAISubCheckStatus = func(ctx context.Context, opts openaisub.Options) (openaisub.Status, error) {
		return openaisub.Status{}, errors.New("status failed")
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "auth", "openai", "status"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 1, code)
	require.Empty(t, out.String())
	require.Contains(t, errOut.String(), "status failed")
}
