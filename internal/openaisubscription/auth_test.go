package openaisubscription

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractAccountIDFromIDToken(t *testing.T) {
	token := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "account-123",
		},
	})

	got := extractAccountID(tokenResponse{IDToken: token})

	assert.Equal(t, "account-123", got)
}

func TestExtractAccountIDFallsBackToOrganization(t *testing.T) {
	token := testJWT(t, map[string]any{
		"organizations": []map[string]any{
			{"id": "org-123"},
		},
	})

	got := extractAccountID(tokenResponse{IDToken: token})

	assert.Equal(t, "org-123", got)
}

func TestBuildAuthorizeURLUsesCodexClientID(t *testing.T) {
	got := buildAuthorizeURL("http://localhost:1455/auth/callback", pkceCodes{
		Verifier:  "verifier",
		Challenge: "challenge",
	}, "state")

	assert.Contains(t, got, "client_id=app_EMoamEEZ73f0CkXaXp7hrann")
	assert.Contains(t, got, "codex_cli_simplified_flow=true")
	assert.Contains(t, got, "originator=codalotl")
}

func testJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]any{"alg": "none"})
	require.NoError(t, err)
	body, err := json.Marshal(claims)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body) + ".sig"
}
