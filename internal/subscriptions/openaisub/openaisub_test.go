package openaisub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoginUsesDeviceCodeFlowAndPersistsAuth(t *testing.T) {
	var requestedDeviceCode map[string]string
	var requestedDeviceToken map[string]string
	var requestedExchange url.Values
	oauthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, oauthTokenPath, r.URL.Path)
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		requestedExchange = r.PostForm
		_, _ = w.Write([]byte(`{
			"access_token":"access-token",
			"refresh_token":"refresh-token",
			"id_token":"` + jwtForAccount(t, "account-id") + `",
			"expires_in":3600
		}`))
	}))
	defer oauthServer.Close()

	issuerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		switch r.URL.Path {
		case deviceCodePath:
			require.NoError(t, json.NewDecoder(r.Body).Decode(&requestedDeviceCode))
			_, _ = w.Write([]byte(`{"device_auth_id":"device-id","user_code":"USER-CODE","interval":1}`))
		case deviceTokenPath:
			require.NoError(t, json.NewDecoder(r.Body).Decode(&requestedDeviceToken))
			_, _ = w.Write([]byte(`{"authorization_code":"auth-code","code_challenge":"challenge","code_verifier":"verifier"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer issuerServer.Close()

	path := filepath.Join(t.TempDir(), "openai_auth.json")
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	var out strings.Builder
	err := Login(context.Background(), LoginOptions{
		Options: Options{
			Path:        path,
			Now:         func() time.Time { return now },
			Issuer:      issuerServer.URL,
			OAuthIssuer: oauthServer.URL,
			Out:         &out,
		},
		NoBrowser: true,
	})
	require.NoError(t, err)

	assert.Equal(t, ClientID, requestedDeviceCode["client_id"])
	assert.Equal(t, "device-id", requestedDeviceToken["device_auth_id"])
	assert.Equal(t, "USER-CODE", requestedDeviceToken["user_code"])
	assert.Equal(t, "authorization_code", requestedExchange.Get("grant_type"))
	assert.Equal(t, "auth-code", requestedExchange.Get("code"))
	assert.Equal(t, issuerServer.URL+"/deviceauth/callback", requestedExchange.Get("redirect_uri"))
	assert.Equal(t, ClientID, requestedExchange.Get("client_id"))
	assert.Equal(t, "verifier", requestedExchange.Get("code_verifier"))
	assert.Contains(t, out.String(), "USER-CODE")

	auth, _, err := loadAuth(Options{Path: path})
	require.NoError(t, err)
	assert.Equal(t, authType, auth.Type)
	assert.Equal(t, "access-token", auth.AccessToken)
	assert.Equal(t, "refresh-token", auth.RefreshToken)
	assert.Equal(t, "account-id", auth.ChatGPTAccountID)
	assert.Equal(t, now.Add(time.Hour), auth.ExpiresAt)
}

func TestLoginOpensBrowserWithVerificationURL(t *testing.T) {
	oauthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"access_token":"access-token",
			"refresh_token":"refresh-token",
			"id_token":"` + jwtForAccount(t, "account-id") + `",
			"expires_in":3600
		}`))
	}))
	defer oauthServer.Close()

	issuerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case deviceCodePath:
			_, _ = w.Write([]byte(`{"device_auth_id":"device-id","user_code":"USER-CODE","verification_uri":"https://verify.example.test"}`))
		case deviceTokenPath:
			_, _ = w.Write([]byte(`{"authorization_code":"auth-code","code_verifier":"verifier"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer issuerServer.Close()

	path := filepath.Join(t.TempDir(), "openai_auth.json")
	var openedURL string
	err := Login(context.Background(), LoginOptions{
		Options: Options{
			Path:        path,
			Issuer:      issuerServer.URL,
			OAuthIssuer: oauthServer.URL,
			OpenBrowser: func(rawURL string) error {
				openedURL = rawURL
				return nil
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "https://verify.example.test", openedURL)
}

func TestCheckStatusRefreshesExpiredAuthFile(t *testing.T) {
	var refreshBody url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, oauthTokenPath, r.URL.Path)
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		refreshBody = r.PostForm
		_, _ = w.Write([]byte(`{
			"access_token":"new-access-token",
			"refresh_token":"new-refresh-token",
			"id_token":"` + jwtForAccount(t, "new-account-id") + `",
			"expires_in":3600
		}`))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "openai_auth.json")
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	writeAuthFile(t, path, authFile{
		Type:             authType,
		AccessToken:      "old-access-token",
		RefreshToken:     "old-refresh-token",
		IDToken:          jwtForAccount(t, "old-account-id"),
		ExpiresAt:        now.Add(-time.Hour),
		ChatGPTAccountID: "old-account-id",
	})

	status, err := CheckStatusWithOptions(context.Background(), Options{
		Path:        path,
		Now:         func() time.Time { return now },
		OAuthIssuer: server.URL,
	})
	require.NoError(t, err)

	assert.True(t, status.LoggedIn)
	assert.Equal(t, path, status.Path)
	assert.Equal(t, "new-account-id", status.ChatGPTAccountID)
	assert.Equal(t, now.Add(time.Hour), status.ExpiresAt)
	assert.Equal(t, ClientID, refreshBody.Get("client_id"))
	assert.Equal(t, "refresh_token", refreshBody.Get("grant_type"))
	assert.Equal(t, "old-refresh-token", refreshBody.Get("refresh_token"))

	auth, _, err := loadAuth(Options{Path: path})
	require.NoError(t, err)
	assert.Equal(t, "new-access-token", auth.AccessToken)
	assert.Equal(t, "new-refresh-token", auth.RefreshToken)
	assert.Equal(t, "new-account-id", auth.ChatGPTAccountID)
}

func TestCheckStatusMissingAndExpiredWithoutRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openai_auth.json")
	status, err := CheckStatusWithOptions(context.Background(), Options{Path: path})
	require.NoError(t, err)
	assert.False(t, status.LoggedIn)
	assert.Equal(t, path, status.Path)

	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	writeAuthFile(t, path, authFile{
		Type:             authType,
		AccessToken:      "access-token",
		ExpiresAt:        now.Add(-time.Hour),
		ChatGPTAccountID: "account-id",
	})
	status, err = CheckStatusWithOptions(context.Background(), Options{
		Path: path,
		Now:  func() time.Time { return now },
	})
	require.NoError(t, err)
	assert.False(t, status.LoggedIn)
	assert.Equal(t, "account-id", status.ChatGPTAccountID)
}

func TestSaveAuthUsesPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode bits are not reliable on Windows")
	}

	dir := filepath.Join(t.TempDir(), "auth")
	path := filepath.Join(dir, "openai_auth.json")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0o644))
	require.NoError(t, os.Chmod(dir, 0o755))
	require.NoError(t, os.Chmod(path, 0o644))

	err := saveAuth(path, authFile{
		Type:             authType,
		AccessToken:      "access-token",
		RefreshToken:     "refresh-token",
		IDToken:          jwtForAccount(t, "account-id"),
		ExpiresAt:        time.Now().Add(time.Hour),
		ChatGPTAccountID: "account-id",
	})
	require.NoError(t, err)

	dirInfo, err := os.Stat(dir)
	require.NoError(t, err)
	fileInfo, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
	assert.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
}

func TestLogoutRemovesAuthAndMissingIsNotError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openai_auth.json")
	writeAuthFile(t, path, authFile{
		Type:             authType,
		AccessToken:      "access-token",
		ExpiresAt:        time.Now().Add(time.Hour),
		ChatGPTAccountID: "account-id",
	})

	require.NoError(t, LogoutWithOptions(Options{Path: path}))
	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, LogoutWithOptions(Options{Path: path}))
}

func TestAccountIDFromJWTReadsNestedAuthClaim(t *testing.T) {
	assert.Equal(t, "account-id", accountIDFromJWT(jwtWithPayload(t, `{"auth":{"chatgpt_account_id":"account-id"}}`)))
}

func TestRequestDeviceCodeUsesAuthIssuerByDefault(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, defaultIssuer+deviceCodePath, req.URL.String())
			return jsonResponse(`{"device_auth_id":"device-id","user_code":"USER-CODE"}`), nil
		}),
	}

	device, err := requestDeviceCode(context.Background(), Options{}, client)
	require.NoError(t, err)
	assert.Equal(t, "device-id", device.DeviceAuthID)
	assert.Equal(t, "USER-CODE", device.UserCode)
}

func writeAuthFile(t *testing.T, path string, auth authFile) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	data, err := json.Marshal(auth)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

func jwtForAccount(t *testing.T, accountID string) string {
	t.Helper()
	return jwtWithPayload(t, `{"chatgpt_account_id":"`+accountID+`"}`)
}

func jwtWithPayload(t *testing.T, payloadJSON string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(payloadJSON))
	return header + "." + payload + "."
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
