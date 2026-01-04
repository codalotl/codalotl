# cli

The `cli` package represents the codalotl CLI. It should be used by a very thin main package - the meat is here.

We assume the app is named `codalotl`.

We use the `internal/q/cli` CLI framework to implement it.

## Commands

Notes:
- Any argument <path/to/pkg> can either use a Go-style package path (ex: `.`; `..`; `./internal/cli`) to a single package OR a relative/absolute dir (ex: `internal/cli`; `/home/proj/codalotl/internal/cli`), with optional trailing `/`.
    - It may NOT use `...` package patterns (if we need this, we'll invent a new identifier for it, for instance: <package_pattern>).

### codalotl -h, codalotl --help

Prints standard usage.

### codalotl

The naked `codalotl` launches the TUI (`internal/tui`).

### codalotl version

Prints the codalotl version to stdout.

### codalotl config

Prints the codalotl configuration to stdout.

### codalotl context public <path/to/pkg>

Prints out the public API of the package (see the `internal/gocodecontext` package).

### codalotl context initial <path/to/pkg>

Prints out the initial context to LLMs for the package (see the `internal/initialcontext` package).

### codalotl context packages [--search <go_regexp>] [--deps]

Prints a list of packages available in the module containing the current working directory, as LLM-ready context (see `internal/gocodecontext.PackageList`).

Notes:
- `--search` filters packages by interpreting `<go_regexp>` as a Go regexp.
- If `--deps` is set, packages from direct (non-`// indirect`) module dependencies are also included.
- The output format is intentionally opaque and may change; callers should treat it as text intended to be copied into an LLM prompt rather than parsed.


## Configuration

This package is responsible for loading a configuation file and passing various configuration to other packages. The configuration is loaded with `internal/q/cascade`. The configuration is loaded and validated for all commands, except those that obviously don't need it, like `version` and `-h`. An invalid configuration prints out a helpful error message and returns with an non-zero exit code.

The config file is loaded with this preference:
- `.codalotl/config.json` (starting from the working directory, recursively checking the parent, until some reasonable stop condition).
- `~/.codalotl/config.json` or `%LOCALAPPDATA%\.codalotl\config.json`.

Config:

```go
// Note to self about how cascade currently maps to fields: it does NOT use json. It's just fieldname lowercase.
type Config struct {
	ProviderKeys          ProviderKeys       `json:"providerkeys"`
	MaxWidth              int                `json:"maxwidth"` // Max width when reflowing documentation. Default to 120
	MaxWidthProvidence    cascade.Providence `json:"-"`
	DisableTelemetry      bool               `json:"disabletelemetry,omitempty"`
	DisableCrashReporting bool               `json:"disablecrashreporting,omitempty"`

	// Optional. If set, use this provider if possible (lower precedence than PreferredModel, though). Allowed values are llmmodel's AllProviderIDs().
	PreferredProvider string `json:"preferredprovider"`

	// Optional. If set, use this model specifically. Allowed values are llmmodel's AvailableModelIDs().
	PreferredModel string `json:"preferredmodel"`
}

// NOTE: separate struct so we can easily test zero value
type ProviderKeys struct {
	OpenAI      string `json:"openai"`
	Anthropic   string `json:"anthropic"`
	XAI         string `json:"xai"`
	Gemini      string `json:"gemini"`
}
```

Notes:
- For now only OpenAI is allowed. If any other model/provider is configured, print out a helpeful error message and exit. But keep this OpenAI limit separate from the main validation, since it's temporary.


## Public API

```go
// Version is the codalotl version. It is a var (not a const) so build tooling can override it.
var Version = "0.1.0"

// In/Out/Err override standard I/O. If nil, defaults are used. Overriding is useful for testing.
//
// Note that if Stdout/Stderr are overridden, we will pass them to other package's functions if they accept them. However, not all will; some packages will probably print to Stdout.
type RunOptions struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Run runs the CLI with args (typically you'd use os.Args).
//
// It returns a recommended exit code (0, 1, or 2) and an error, if any:
//   - 0 -> err == nil
//   - 1 -> err != nil, but the structure of args is sound (flags are correct, etc).
//   - 2 -> err != nil, args parse error or misuse of flags, etc.
//
// Note that in cases of errors, Run has already displayed an error message to opts.Err || Stderr. Callers may use os.Exit with the exit code.
func Run(args []string, opts *RunOptions) (int, error)
```
