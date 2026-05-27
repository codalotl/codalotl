# openaisub

openaisub manages OpenAI ChatGPT subscription authentication for codalotl.

It owns OpenAI-specific device login, token refresh, credential persistence, and status checks. It does not send model response requests.

## Behavior

- Uses OpenAI app client ID `app_EMoamEEZ73f0CkXaXp7hrann`.
- Stores auth in `~/.codalotl/openai_auth.json` by default, with private file/directory permissions.
- Login starts OpenAI device authorization, optionally opens a browser, waits for approval, exchanges for tokens, and saves usable credentials.
- Status loads saved auth, refreshes when possible, and reports whether saved credentials are currently usable.
- Startup refreshes expired default saved auth when possible before syncing subscription auth.
- Logout removes saved auth. Missing saved auth is not an error.
- Options support alternate auth paths, HTTP clients, clock functions, auth endpoint URLs, browser-opening behavior, and user-visible output.
- Usable saved/login/refreshed auth registers OpenAI subscription auth with `llmmodel`.
	- Access token is used as bearer auth.
	- ChatGPT account ID is sent with subscription requests.
	- Registered endpoint targets ChatGPT Codex-compatible Responses.
	- Registered subscription requires no-store Responses semantics and root instructions.
- Unusable status and logout clear registered OpenAI subscription auth.

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
func Login(ctx context.Context, opts LoginOptions) error

// Logout removes saved OpenAI subscription auth.
func Logout() error

// LogoutWithOptions removes saved OpenAI subscription auth.
func LogoutWithOptions(opts Options) error

// CheckStatus reports saved OpenAI subscription auth status.
func CheckStatus(ctx context.Context) (Status, error)

// CheckStatusWithOptions reports saved OpenAI subscription auth status.
func CheckStatusWithOptions(ctx context.Context, opts Options) (Status, error)
```
