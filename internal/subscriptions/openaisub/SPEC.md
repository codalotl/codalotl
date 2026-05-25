# openaisub

openaisub manages OpenAI ChatGPT subscription authentication for codalotl.

It owns OpenAI-specific device login, token refresh, credential persistence, and translation into `llmmodel` subscription auth.

## Behavior

- Uses OpenAI app client ID `app_EMoamEEZ73f0CkXaXp7hrann`.
- Stores auth in `~/.codalotl/openai_auth.json` by default, with private file/directory permissions.
- Initializes by loading saved auth, refreshing when possible, and configuring `llmmodel` OpenAI subscription auth when valid.
- Logout removes saved auth and clears OpenAI subscription auth from `llmmodel`.
- Status reports whether saved auth is currently usable.
- Configures ChatGPT Codex subscription auth with no-store and root-instructions requirements.

## Public API

```go
const ClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

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

type LoginOptions struct {
	Options
	NoBrowser bool
}

type Status struct {
	LoggedIn         bool
	Path             string
	ChatGPTAccountID string
	ExpiresAt        time.Time
}

// DefaultPath returns the default OpenAI subscription auth file path.
func DefaultPath() string

// Init loads saved OpenAI subscription auth and configures llmmodel when usable.
func Init(ctx context.Context) error

// InitWithOptions loads saved OpenAI subscription auth and configures llmmodel when usable.
func InitWithOptions(ctx context.Context, opts Options) error

// Login runs the OpenAI subscription device login flow and saves usable auth.
func Login(ctx context.Context, opts LoginOptions) error

// Logout removes saved OpenAI subscription auth and clears llmmodel subscription auth.
func Logout() error

// LogoutWithOptions removes saved OpenAI subscription auth and clears llmmodel subscription auth.
func LogoutWithOptions(opts Options) error

// CheckStatus reports saved OpenAI subscription auth status.
func CheckStatus(ctx context.Context) (Status, error)

// CheckStatusWithOptions reports saved OpenAI subscription auth status.
func CheckStatusWithOptions(ctx context.Context, opts Options) (Status, error)
```
