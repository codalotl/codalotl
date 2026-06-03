# openaisub

openaisub manages OpenAI ChatGPT subscription authentication for codalotl.

It owns OpenAI-specific device login, token refresh, credential persistence, and status checks. It does not send model response requests.

## Behavior

- Uses OpenAI app client ID `app_EMoamEEZ73f0CkXaXp7hrann`.
- Stores auth in `~/.codalotl/openai_auth.json` by default, with private file/directory permissions.
- Login starts OpenAI device authorization, optionally opens a browser, waits for approval, exchanges for tokens, and saves usable credentials.
- Status loads saved auth, refreshes when possible, and reports whether saved credentials are currently usable.
- Package initialization locally syncs usable default saved auth without network I/O.
- Startup paths needing subscription-backed model availability explicitly refresh expired default saved auth when possible.
- Logout removes saved auth. Missing saved auth is not an error.
- Options support alternate auth paths, HTTP clients, clock functions, auth endpoint URLs, browser-opening behavior, and user-visible output.
- Default saved auth presence requires OpenAI subscription auth while the default auth file exists.
- Usable saved/login/refreshed auth registers OpenAI subscription auth with `llmmodel`.
	- Access token is used as bearer auth.
	- ChatGPT account ID is sent with subscription requests.
	- Registered endpoint targets ChatGPT Codex-compatible Responses.
	- Registered subscription requires no-store Responses semantics and root instructions.
- Unusable status clears registered OpenAI subscription auth, but default saved auth suppresses OpenAI API-key fallback while present.

## Public API

```go
// ClientID is the OpenAI app client ID used for ChatGPT subscription auth.
const ClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

// Options configures OpenAI subscription auth operations.
type Options struct {
	Path        string
	HTTPClient  *http.Client
	Now         func() time.Time
	Issuer      string
	OAuthIssuer string
	OpenBrowser func(string) error
	Out         io.Writer
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

// DefaultPath returns the default OpenAI subscription auth file path.
func DefaultPath() string

// Login runs the OpenAI subscription device login flow and saves usable auth.
//
// When opts uses DefaultPath, a successful login also configures llmmodel's OpenAI provider subscription auth. Logins saved to a non-default Options.Path do not
// affect llmmodel provider subscription auth.
func Login(ctx context.Context, opts LoginOptions) error

// Logout removes saved OpenAI subscription auth.
func Logout() error

// LogoutWithOptions removes saved OpenAI subscription auth.
func LogoutWithOptions(opts Options) error

// CheckStatus reports default saved OpenAI subscription auth status and syncs llmmodel's OpenAI provider subscription auth.
func CheckStatus(ctx context.Context) (Status, error)

// CheckStatusWithOptions reports saved OpenAI subscription auth status.
//
// It refreshes expired saved credentials when a refresh token is available. When opts uses DefaultPath, it also syncs llmmodel's OpenAI provider subscription auth;
// a missing, invalid, or unrefreshable default auth file clears that provider subscription. A non-default Options.Path reports and refreshes only that file and
// does not affect llmmodel provider subscription auth.
func CheckStatusWithOptions(ctx context.Context, opts Options) (Status, error)

// RefreshDefaultProviderSubscription refreshes and syncs default saved auth for startup availability.
//
// It is the explicit startup hook for callers that need to refresh expired default credentials before model selection or requests depend on subscription auth. Package
// initialization only loads already usable default saved auth.
func RefreshDefaultProviderSubscription(ctx context.Context) error
```
