package openaisub

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/q/cascade"
)

// ClientID is the OpenAI app client ID used for ChatGPT subscription auth.
const ClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

const (
	authType            = "openai_subscription"
	defaultIssuer       = "https://auth.openai.com"
	defaultOAuthIssuer  = "https://auth.openai.com"
	defaultCodexBaseURL = "https://chatgpt.com/backend-api/codex"
	deviceCodePath      = "/api/accounts/deviceauth/usercode"
	deviceTokenPath     = "/api/accounts/deviceauth/token"
	oauthTokenPath      = "/oauth/token"
	defaultPollTimeout  = 15 * time.Minute
	defaultPollInterval = 5 * time.Second
	expiryRefreshSlack  = time.Minute
)

// Options configures OpenAI subscription auth operations.
type Options struct {
	Path         string
	HTTPClient   *http.Client
	Now          func() time.Time
	Issuer       string
	OAuthIssuer  string
	CodexBaseURL string
	OpenBrowser  func(string) error
	Out          io.Writer
}

// LoginOptions configures the OpenAI subscription device login flow.
type LoginOptions struct {
	Options
	NoBrowser bool
}

// Status describes saved OpenAI subscription auth status.
type Status struct {
	LoggedIn         bool
	Path             string
	ChatGPTAccountID string
	ExpiresAt        time.Time
}

type authFile struct {
	Type             string    `json:"type"`
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token,omitempty"`
	IDToken          string    `json:"id_token,omitempty"`
	ExpiresAt        time.Time `json:"expires_at"`
	ChatGPTAccountID string    `json:"chatgpt_account_id"`
}

// DefaultPath returns the default OpenAI subscription auth file path.
func DefaultPath() string {
	return cascade.ExpandPath("~/.codalotl/openai_auth.json")
}

// Init loads saved OpenAI subscription auth and configures llmmodel when usable.
func Init(ctx context.Context) error {
	return InitWithOptions(ctx, Options{})
}

// InitWithOptions loads saved OpenAI subscription auth and configures llmmodel when usable.
func InitWithOptions(ctx context.Context, opts Options) error {
	auth, path, err := loadAuth(opts)
	if errors.Is(err, os.ErrNotExist) {
		llmmodel.ClearProviderSubscription(llmmodel.ProviderIDOpenAI)
		return nil
	}
	if err != nil {
		llmmodel.ClearProviderSubscription(llmmodel.ProviderIDOpenAI)
		return err
	}

	now := nowFunc(opts)()
	if auth.expired(now) && auth.RefreshToken != "" {
		refreshed, err := refresh(ctx, opts, auth)
		if err == nil {
			auth = refreshed
			if err := saveAuth(path, auth); err != nil {
				return err
			}
		}
	}

	now = nowFunc(opts)()
	if !auth.valid(now) {
		llmmodel.ClearProviderSubscription(llmmodel.ProviderIDOpenAI)
		return nil
	}
	configureSubscription(auth, codexBaseURL(opts))
	return nil
}

// Login runs the OpenAI subscription device login flow and saves usable auth.
func Login(ctx context.Context, opts LoginOptions) error {
	path := authPath(opts.Options)
	client := httpClient(opts.Options)
	out := opts.Out
	if out == nil {
		out = io.Discard
	}

	device, err := requestDeviceCode(ctx, opts.Options, client)
	if err != nil {
		return err
	}
	verifyURL := strings.TrimRight(issuer(opts.Options), "/") + "/codex/device"
	if _, err := fmt.Fprintf(out, "Open %s and enter code %s\n", verifyURL, device.UserCode); err != nil {
		return err
	}
	if !opts.NoBrowser {
		openBrowser := opts.OpenBrowser
		if openBrowser == nil {
			openBrowser = openURL
		}
		_ = openBrowser(verifyURL)
	}

	code, err := pollDeviceToken(ctx, opts.Options, client, device)
	if err != nil {
		return err
	}
	tokens, err := exchangeCode(ctx, opts.Options, client, code)
	if err != nil {
		return err
	}
	auth := authFile{
		Type:             authType,
		AccessToken:      tokens.AccessToken,
		RefreshToken:     tokens.RefreshToken,
		IDToken:          tokens.IDToken,
		ExpiresAt:        nowFunc(opts.Options)().Add(tokens.expiresIn()),
		ChatGPTAccountID: tokens.ChatGPTAccountID,
	}
	auth = auth.normalized()
	if !auth.valid(nowFunc(opts.Options)()) {
		return errors.New("OpenAI subscription login did not return usable credentials")
	}
	if err := saveAuth(path, auth); err != nil {
		return err
	}
	configureSubscription(auth, codexBaseURL(opts.Options))
	_, err = fmt.Fprintln(out, "Logged in to OpenAI subscription.")
	return err
}

// Logout removes saved OpenAI subscription auth and clears llmmodel subscription auth.
func Logout() error {
	return LogoutWithOptions(Options{})
}

// LogoutWithOptions removes saved OpenAI subscription auth and clears llmmodel subscription auth.
func LogoutWithOptions(opts Options) error {
	path := authPath(opts)
	llmmodel.ClearProviderSubscription(llmmodel.ProviderIDOpenAI)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// CheckStatus reports saved OpenAI subscription auth status.
func CheckStatus(ctx context.Context) (Status, error) {
	return CheckStatusWithOptions(ctx, Options{})
}

// CheckStatusWithOptions reports saved OpenAI subscription auth status.
func CheckStatusWithOptions(ctx context.Context, opts Options) (Status, error) {
	if err := InitWithOptions(ctx, opts); err != nil {
		return Status{Path: authPath(opts)}, err
	}
	auth, path, err := loadAuth(opts)
	if errors.Is(err, os.ErrNotExist) {
		return Status{Path: path}, nil
	}
	if err != nil {
		return Status{Path: path}, err
	}
	return Status{
		LoggedIn:         auth.valid(nowFunc(opts)()),
		Path:             path,
		ChatGPTAccountID: auth.ChatGPTAccountID,
		ExpiresAt:        auth.ExpiresAt,
	}, nil
}

func configureSubscription(auth authFile, baseURL string) {
	llmmodel.SetProviderSubscription(llmmodel.ProviderIDOpenAI, llmmodel.ProviderSubscription{
		ProviderID:       llmmodel.ProviderIDOpenAI,
		AccessToken:      auth.AccessToken,
		AccountID:        auth.ChatGPTAccountID,
		APIEndpointURL:   baseURL,
		ExpiresAt:        auth.ExpiresAt,
		RequiresNoStore:  true,
		RootInstructions: true,
	})
}

func loadAuth(opts Options) (authFile, string, error) {
	path := authPath(opts)
	data, err := os.ReadFile(path)
	if err != nil {
		return authFile{}, path, err
	}
	var auth authFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return authFile{}, path, fmt.Errorf("read OpenAI subscription auth: %w", err)
	}
	return auth.normalized(), path, nil
}

func saveAuth(path string, auth authFile) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if dir != "." {
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func (auth authFile) normalized() authFile {
	if auth.ChatGPTAccountID == "" {
		auth.ChatGPTAccountID = accountIDFromJWT(auth.IDToken)
	}
	return auth
}

func (auth authFile) valid(now time.Time) bool {
	return auth.Type == authType &&
		strings.TrimSpace(auth.AccessToken) != "" &&
		strings.TrimSpace(auth.ChatGPTAccountID) != "" &&
		!auth.expired(now)
}

func (auth authFile) expired(now time.Time) bool {
	return !auth.ExpiresAt.IsZero() && !auth.ExpiresAt.After(now.Add(expiryRefreshSlack))
}

type deviceCodeResponse struct {
	DeviceAuthID string
	UserCode     string
	Interval     time.Duration
}

func (r *deviceCodeResponse) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	_ = json.Unmarshal(raw["device_auth_id"], &r.DeviceAuthID)
	_ = json.Unmarshal(raw["user_code"], &r.UserCode)
	if r.UserCode == "" {
		_ = json.Unmarshal(raw["usercode"], &r.UserCode)
	}
	r.Interval = defaultPollInterval
	if rawInterval, ok := raw["interval"]; ok {
		var n int64
		if err := json.Unmarshal(rawInterval, &n); err == nil && n > 0 {
			r.Interval = time.Duration(n) * time.Second
			return nil
		}
		var s string
		if err := json.Unmarshal(rawInterval, &s); err == nil {
			if parsed, err := strconv.ParseInt(s, 10, 64); err == nil && parsed > 0 {
				r.Interval = time.Duration(parsed) * time.Second
			}
		}
	}
	return nil
}

type deviceTokenResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeChallenge     string `json:"code_challenge"`
	CodeVerifier      string `json:"code_verifier"`
}

type tokenResponse struct {
	IDToken          string `json:"id_token"`
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int64  `json:"expires_in"`
	ChatGPTAccountID string `json:"chatgpt_account_id"`
}

func (r tokenResponse) expiresIn() time.Duration {
	if r.ExpiresIn <= 0 {
		return 7 * 24 * time.Hour
	}
	return time.Duration(r.ExpiresIn) * time.Second
}

func requestDeviceCode(ctx context.Context, opts Options, client *http.Client) (deviceCodeResponse, error) {
	var device deviceCodeResponse
	body := map[string]string{"client_id": ClientID}
	if err := postJSON(ctx, client, strings.TrimRight(issuer(opts), "/")+deviceCodePath, body, &device); err != nil {
		return device, err
	}
	if device.DeviceAuthID == "" || device.UserCode == "" {
		return device, errors.New("OpenAI device code response missing required fields")
	}
	return device, nil
}

func pollDeviceToken(ctx context.Context, opts Options, client *http.Client, device deviceCodeResponse) (deviceTokenResponse, error) {
	var code deviceTokenResponse
	body := map[string]string{
		"device_auth_id": device.DeviceAuthID,
		"user_code":      device.UserCode,
	}
	ctx, cancel := context.WithTimeout(ctx, defaultPollTimeout)
	defer cancel()

	for {
		reqBody, err := json.Marshal(body)
		if err != nil {
			return code, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(issuer(opts), "/")+deviceTokenPath, bytes.NewReader(reqBody))
		if err != nil {
			return code, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return code, err
		}
		data, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			return code, readErr
		}
		if closeErr != nil {
			return code, closeErr
		}
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusTooEarly {
			if err := sleepContext(ctx, device.Interval); err != nil {
				return code, err
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return code, fmt.Errorf("OpenAI device token poll failed: %s", resp.Status)
		}
		if err := json.Unmarshal(data, &code); err != nil {
			return code, err
		}
		if code.AuthorizationCode == "" || code.CodeVerifier == "" {
			return code, errors.New("OpenAI device token response missing required fields")
		}
		return code, nil
	}
}

func exchangeCode(ctx context.Context, opts Options, client *http.Client, code deviceTokenResponse) (tokenResponse, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code.AuthorizationCode)
	values.Set("redirect_uri", strings.TrimRight(issuer(opts), "/")+"/deviceauth/callback")
	values.Set("client_id", ClientID)
	values.Set("code_verifier", code.CodeVerifier)

	var tokens tokenResponse
	if err := postForm(ctx, client, strings.TrimRight(oauthIssuer(opts), "/")+oauthTokenPath, values, &tokens); err != nil {
		return tokens, err
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.IDToken == "" {
		return tokens, errors.New("OpenAI token exchange response missing required fields")
	}
	return tokens, nil
}

func refresh(ctx context.Context, opts Options, auth authFile) (authFile, error) {
	values := url.Values{}
	values.Set("client_id", ClientID)
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", auth.RefreshToken)

	var tokens tokenResponse
	if err := postForm(ctx, httpClient(opts), strings.TrimRight(oauthIssuer(opts), "/")+oauthTokenPath, values, &tokens); err != nil {
		return auth, err
	}
	if tokens.AccessToken != "" {
		auth.AccessToken = tokens.AccessToken
	}
	if tokens.RefreshToken != "" {
		auth.RefreshToken = tokens.RefreshToken
	}
	if tokens.IDToken != "" {
		auth.IDToken = tokens.IDToken
		if accountID := accountIDFromJWT(tokens.IDToken); accountID != "" {
			auth.ChatGPTAccountID = accountID
		}
	}
	if tokens.ChatGPTAccountID != "" {
		auth.ChatGPTAccountID = tokens.ChatGPTAccountID
	}
	auth.ExpiresAt = nowFunc(opts)().Add(tokens.expiresIn())
	return auth.normalized(), nil
}

func postJSON(ctx context.Context, client *http.Client, endpoint string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return doDecode(client, req, out)
}

func postForm(ctx context.Context, client *http.Client, endpoint string, values url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return doDecode(client, req, out)
}

func doDecode(client *http.Client, req *http.Request, out any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s failed: %s", req.URL.Path, resp.Status)
	}
	return json.Unmarshal(data, out)
}

func accountIDFromJWT(jwt string) string {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return ""
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(data, &claims); err != nil {
		return ""
	}
	if accountID, ok := claims["chatgpt_account_id"].(string); ok {
		return accountID
	}
	if authClaims, ok := claims["auth"].(map[string]any); ok {
		if accountID, ok := authClaims["chatgpt_account_id"].(string); ok {
			return accountID
		}
	}
	return ""
}

func authPath(opts Options) string {
	if opts.Path != "" {
		return cascade.ExpandPath(opts.Path)
	}
	return DefaultPath()
}

func httpClient(opts Options) *http.Client {
	if opts.HTTPClient != nil {
		return opts.HTTPClient
	}
	return http.DefaultClient
}

func nowFunc(opts Options) func() time.Time {
	if opts.Now != nil {
		return opts.Now
	}
	return time.Now
}

func issuer(opts Options) string {
	if opts.Issuer != "" {
		return opts.Issuer
	}
	return defaultIssuer
}

func oauthIssuer(opts Options) string {
	if opts.OAuthIssuer != "" {
		return opts.OAuthIssuer
	}
	return defaultOAuthIssuer
}

func codexBaseURL(opts Options) string {
	if opts.CodexBaseURL != "" {
		return opts.CodexBaseURL
	}
	return defaultCodexBaseURL
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func openURL(rawURL string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", rawURL).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	default:
		return exec.Command("xdg-open", rawURL).Start()
	}
}
