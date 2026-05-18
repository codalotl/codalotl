package openaisubscription

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	ClientID      = "app_EMoamEEZ73f0CkXaXp7hrann"
	Issuer        = "https://auth.openai.com"
	CodexAPIBase  = "https://chatgpt.com/backend-api/codex"
	Originator    = "codalotl"
	preferredPort = 1455
	fallbackPort  = 1457
)

type Credentials struct {
	Type             string    `json:"type"`
	IDToken          string    `json:"id_token"`
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	ExpiresAt        time.Time `json:"expires_at"`
	ChatGPTAccountID string    `json:"chatgpt_account_id,omitempty"`
}

type Auth struct {
	AccessToken      string
	ChatGPTAccountID string
}

type Status struct {
	Path             string
	SignedIn         bool
	ChatGPTAccountID string
	ExpiresAt        time.Time
}

type tokenResponse struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type pkceCodes struct {
	Verifier  string
	Challenge string
}

func DefaultCredentialsPath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".codalotl", "openai_auth.json")
	}
	return filepath.Join(home, ".codalotl", "openai_auth.json")
}

func HasCredentials() bool {
	creds, err := LoadCredentials()
	return err == nil && credentialsUsable(creds)
}

func LoadCredentials() (Credentials, error) {
	path := DefaultCredentialsPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, err
	}
	var creds Credentials
	if err := json.Unmarshal(raw, &creds); err != nil {
		return Credentials{}, err
	}
	if !credentialsUsable(creds) {
		return Credentials{}, fmt.Errorf("openai subscription credentials are incomplete")
	}
	return creds, nil
}

func SaveCredentials(creds Credentials) error {
	if creds.Type == "" {
		creds.Type = "openai_subscription"
	}
	path := DefaultCredentialsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0600)
}

func DeleteCredentials() error {
	err := os.Remove(DefaultCredentialsPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func CurrentStatus() Status {
	path := DefaultCredentialsPath()
	creds, err := LoadCredentials()
	if err != nil {
		return Status{Path: path}
	}
	return Status{
		Path:             path,
		SignedIn:         true,
		ChatGPTAccountID: creds.ChatGPTAccountID,
		ExpiresAt:        creds.ExpiresAt,
	}
}

func ResolveAuth(ctx context.Context) (Auth, error) {
	creds, err := LoadCredentials()
	if err != nil {
		return Auth{}, err
	}
	if time.Until(creds.ExpiresAt) <= 2*time.Minute {
		creds, err = RefreshCredentials(ctx, creds)
		if err != nil {
			return Auth{}, err
		}
	}
	return Auth{
		AccessToken:      creds.AccessToken,
		ChatGPTAccountID: creds.ChatGPTAccountID,
	}, nil
}

func RefreshCredentials(ctx context.Context, creds Credentials) (Credentials, error) {
	if strings.TrimSpace(creds.RefreshToken) == "" {
		return Credentials{}, fmt.Errorf("openai subscription credentials are missing refresh token")
	}
	tokens, err := refreshAccessToken(ctx, creds.RefreshToken)
	if err != nil {
		return Credentials{}, err
	}
	return saveTokenResponse(tokens)
}

func Login(ctx context.Context, out io.Writer, openBrowser bool) (Credentials, error) {
	pkce, err := generatePKCE()
	if err != nil {
		return Credentials{}, err
	}
	state, err := randomBase64URL(32)
	if err != nil {
		return Credentials{}, err
	}

	ln, port, err := listenOnLoginPort()
	if err != nil {
		return Credentials{}, err
	}
	defer ln.Close()

	redirectURI := fmt.Sprintf("http://localhost:%d/auth/callback", port)
	authURL := buildAuthorizeURL(redirectURI, pkce, state)

	if out != nil {
		fmt.Fprintf(out, "Open this URL to sign in with ChatGPT:\n%s\n\n", authURL)
	}
	if openBrowser {
		_ = openURL(authURL)
	}

	result := make(chan loginResult, 1)
	server := &http.Server{
		Handler: loginHandler(ctx, redirectURI, pkce, state, result),
	}
	go func() {
		err := server.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			result <- loginResult{err: err}
		}
	}()

	select {
	case <-ctx.Done():
		_ = server.Shutdown(context.Background())
		return Credentials{}, ctx.Err()
	case res := <-result:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		return res.creds, res.err
	}
}

type loginResult struct {
	creds Credentials
	err   error
}

func loginHandler(ctx context.Context, redirectURI string, pkce pkceCodes, state string, result chan<- loginResult) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("state"); got != state {
			http.Error(w, "State mismatch", http.StatusBadRequest)
			result <- loginResult{err: fmt.Errorf("oauth state mismatch")}
			return
		}
		if msg := q.Get("error_description"); msg != "" {
			http.Error(w, msg, http.StatusForbidden)
			result <- loginResult{err: fmt.Errorf("oauth callback failed: %s", msg)}
			return
		}
		if code := q.Get("error"); code != "" {
			http.Error(w, code, http.StatusForbidden)
			result <- loginResult{err: fmt.Errorf("oauth callback failed: %s", code)}
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			result <- loginResult{err: fmt.Errorf("oauth callback missing authorization code")}
			return
		}
		tokens, err := exchangeCodeForTokens(ctx, code, redirectURI, pkce)
		if err != nil {
			http.Error(w, "Token exchange failed", http.StatusBadGateway)
			result <- loginResult{err: err}
			return
		}
		creds, err := saveTokenResponse(tokens)
		if err != nil {
			http.Error(w, "Could not save credentials", http.StatusInternalServerError)
			result <- loginResult{err: err}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><title>Codalotl signed in</title><p>Authorization successful. You can close this window and return to Codalotl.</p>")
		result <- loginResult{creds: creds}
	})
	mux.HandleFunc("/cancel", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "Login cancelled")
		result <- loginResult{err: fmt.Errorf("login cancelled")}
	})
	return mux
}

func saveTokenResponse(tokens tokenResponse) (Credentials, error) {
	expiresIn := tokens.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	creds := Credentials{
		Type:             "openai_subscription",
		IDToken:          tokens.IDToken,
		AccessToken:      tokens.AccessToken,
		RefreshToken:     tokens.RefreshToken,
		ExpiresAt:        time.Now().Add(time.Duration(expiresIn) * time.Second),
		ChatGPTAccountID: extractAccountID(tokens),
	}
	if creds.RefreshToken == "" {
		if existing, err := LoadCredentials(); err == nil {
			creds.RefreshToken = existing.RefreshToken
		}
	}
	if err := SaveCredentials(creds); err != nil {
		return Credentials{}, err
	}
	return creds, nil
}

func exchangeCodeForTokens(ctx context.Context, code string, redirectURI string, pkce pkceCodes) (tokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {ClientID},
		"code_verifier": {pkce.Verifier},
	}
	return postTokenForm(ctx, form)
}

func refreshAccessToken(ctx context.Context, refreshToken string) (tokenResponse, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {ClientID},
	}
	return postTokenForm(ctx, form)
}

func postTokenForm(ctx context.Context, form url.Values) (tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, Issuer+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return tokenResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tokenResponse{}, fmt.Errorf("openai token endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var tokens tokenResponse
	if err := json.Unmarshal(raw, &tokens); err != nil {
		return tokenResponse{}, err
	}
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return tokenResponse{}, fmt.Errorf("openai token endpoint returned no access token")
	}
	return tokens, nil
}

func buildAuthorizeURL(redirectURI string, pkce pkceCodes, state string) string {
	q := url.Values{
		"response_type":              {"code"},
		"client_id":                  {ClientID},
		"redirect_uri":               {redirectURI},
		"scope":                      {"openid profile email offline_access"},
		"code_challenge":             {pkce.Challenge},
		"code_challenge_method":      {"S256"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"state":                      {state},
		"originator":                 {Originator},
	}
	return Issuer + "/oauth/authorize?" + q.Encode()
}

func listenOnLoginPort() (net.Listener, int, error) {
	for _, port := range []int{preferredPort, fallbackPort} {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return ln, port, nil
		}
	}
	return nil, 0, fmt.Errorf("could not listen on localhost ports %d or %d", preferredPort, fallbackPort)
}

func generatePKCE() (pkceCodes, error) {
	verifier, err := randomPKCEVerifier(43)
	if err != nil {
		return pkceCodes{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	return pkceCodes{
		Verifier:  verifier,
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]),
	}, nil
}

func randomPKCEVerifier(length int) (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return string(buf), nil
}

func randomBase64URL(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func openURL(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}

func credentialsUsable(creds Credentials) bool {
	return strings.TrimSpace(creds.AccessToken) != "" || strings.TrimSpace(creds.RefreshToken) != ""
}

type jwtClaims struct {
	ChatGPTAccountID string `json:"chatgpt_account_id"`
	Organizations    []struct {
		ID string `json:"id"`
	} `json:"organizations"`
	OpenAIAuth struct {
		ChatGPTAccountID string `json:"chatgpt_account_id"`
	} `json:"https://api.openai.com/auth"`
}

func extractAccountID(tokens tokenResponse) string {
	for _, token := range []string{tokens.IDToken, tokens.AccessToken} {
		claims, ok := parseJWTClaims(token)
		if !ok {
			continue
		}
		if claims.ChatGPTAccountID != "" {
			return claims.ChatGPTAccountID
		}
		if claims.OpenAIAuth.ChatGPTAccountID != "" {
			return claims.OpenAIAuth.ChatGPTAccountID
		}
		if len(claims.Organizations) > 0 && claims.Organizations[0].ID != "" {
			return claims.Organizations[0].ID
		}
	}
	return ""
}

func parseJWTClaims(token string) (jwtClaims, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return jwtClaims{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtClaims{}, false
	}
	var claims jwtClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return jwtClaims{}, false
	}
	return claims, true
}
