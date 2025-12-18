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
