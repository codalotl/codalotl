# cli

`cli` is a small standard library for building command-line applications with:
- Nested subcommands (a command tree)
- GNU-style flags (`--flag`, `-f`, `--flag=value`)
- Positional args (validated, passed to handlers)
- Automatic help/usage output

The core model is “git/go style”: a root program name, then a command path, then args/flags. For example:
- `git checkout mybranch`
- `go test . -v -run=TestThing`
- `./mycodingagent doc add ./some/pkg`

In help/usage output, the displayed program name is `root.Name`.

## Usage

```go
root := &cli.Command{
	Name:  "mycodingagent",
	Short: "A coding agent",
}

verbose := root.PersistentFlags().Bool("verbose", 'v', false, "Enable verbose logging")

doc := &cli.Command{
	Name:  "doc",
	Short: "Documentation tools",
}

add := &cli.Command{
	Name:  "add",
	Short: "Add docs for a package",
	Args:  cli.ExactArgs(1),
	Run: func(c *cli.Context) error {
		pkg := c.Args[0]
		if *verbose {
			fmt.Fprintln(c.Err, "adding docs for", pkg)
		}
		// ...
		return nil
	},
}

fix := &cli.Command{
	Name:  "fix",
	Short: "Fix docs for a package",
	Args:  cli.ExactArgs(1),
	Run: func(c *cli.Context) error {
		// ...
		return nil
	},
}

doc.AddCommand(add, fix)
root.AddCommand(doc)

os.Exit(cli.Run(context.Background(), root, cli.Options{
	Args: os.Args[1:],
}))
```

## Concepts

### Command Tree (Namespacing)

Commands form a tree rooted at `root`. Each command has a `Name` and can have children.

Invocation selects the *deepest matching* command path:
- `mycodingagent doc add ./some/pkg` selects the `doc add` command and passes `./some/pkg` as a positional arg.

A command can be:
- Runnable: `Run != nil`
- A namespace: it has child commands

Both are allowed (e.g. a command that has subcommands but also does something when invoked directly).

If a selected command has no handler (`Run == nil`), it requires a subcommand. Invoking it directly is a usage error (it prints that command’s help/usage).

#### Command Selection (Resolution)

Command selection proceeds left-to-right from the root command:
- A token matching a child command name or alias descends into that child.
- Flag tokens are parsed against the *current* command (see below) and do not affect command selection.
- `--` ends both command selection and flag parsing; everything after is positional args for the selected command.
- The first non-flag token that does not match a child command ends command selection. After command selection ends, later tokens are never considered subcommand names (but flags may still appear; see below).

This implies a predictable rule of thumb: subcommand names are read until the first “real argument”, and later tokens that happen to equal a subcommand name are treated as positional args.

Namespace-only commands:
- If the final selected command is not runnable (`Run == nil`), it does not accept positional args.
- If there are remaining tokens (including after `--`), the invocation is a usage error:
  - If there is at least one remaining token, it is treated as an *unknown subcommand* of the selected command.
  - If there are no remaining tokens, it is a *missing required subcommand* error.

### Flags

Each command has two flag sets:
- `Flags()` are local to that command.
- `PersistentFlags()` apply to the command and all of its descendants.

Flag parsing is intended to be familiar to users of Git/Cobra/pflag:
- Long flags: `--name`, `--name=value`
- Short flags: `-n`, `-n=value`
- Flag values may also be provided as the next token: `--name value`, `-n value`.
- Flags may be interspersed with positional args.
- `--` ends flag parsing; everything after is positional args for the executed command.

Flag value rules:
- Bool flags set to `true` when provided without a value (e.g. `--verbose`, `-v`).
- Non-bool flags require an explicit value; providing `--name` / `-n` without a value is a usage error.
- If a flag is provided multiple times, the last value wins.

Placement rules (to keep parsing predictable):
- Persistent flags may appear anywhere after the program name (until `--`).
- Local flags should appear after their command’s name appears in argv (typically after the full command path).

In other words: when parsing flags, the active flag set is the union of:
- All persistent flags on the path from the root to the currently-selected command, and
- The local flags of the currently-selected command.

If a local flag for a descendant command appears before that descendant command is selected, it is treated as an unknown flag (usage error). To pass a positional argument that begins with `-`, use `--`.

### Positional Args

After command selection and flag parsing, the remaining tokens are positional args for the selected command.

If `Command.Args` is non-nil, it is called to validate the args before `Run` is invoked.
If it returns a non-nil error, the error is treated as a usage error by default (exit code `2`, prints the executed command’s usage), unless the error implements `ExitCoder` with a non-`2` exit code.

### Help / Usage

Every command supports `-h` / `--help`. When requested, `cli.Run` prints help for the relevant command and does not run a handler.

By default, help/usage is generated from the command tree (names, short/long descriptions, flags, and direct subcommands).

Help resolution:
- If `-h/--help` is encountered while parsing, help is printed for the deepest command selected *so far*.
- Help output is written to `Options.Out`.
- `-h`/`--help` are built-in and reserved (they do not need to be registered on a `FlagSet`).
- After `--`, `-h/--help` are treated as positional args (i.e. they do not trigger help).

Usage errors:
- Usage/error output is written to `Options.Err`.
- For unknown subcommands, usage is printed for the nearest existing parent command.
- For unknown flags or arg validation failures, usage is printed for the command being executed.
- For usage errors, an error message is also printed to `Options.Err` and includes the specific reason:
  - For `UsageError`, the message includes `UsageError.Message`.
  - Otherwise, the message includes the triggering error string.
  - For unknown flags/subcommands, the message includes the unknown token verbatim.

Help/usage output is intended to be stable for testing:
- It is plain text (no ANSI color/terminal control sequences) and ends with a trailing newline.
- When listing subcommands or flags, ordering is deterministic (subcommands by `Name`, flags by long name).

## Exit Codes

`cli.Run` never calls `os.Exit`. It returns an exit code suitable for `os.Exit(...)`.

Core exit code policy:
- `0`: success (including printing help)
- `2`: usage error (unknown subcommand/flag, arg validation failure, missing required subcommand)
- `1`: handler error (a command’s `Run` returned a non-usage error)

Handlers can control exit codes by returning an error that implements `ExitCoder`. If a handler returns an `ExitCoder` with exit code `2`, `cli.Run` treats it as a usage error (i.e., it prints usage/help for the executed command).

On a handler error (exit code 1), `cli.Run` prints the error message to `Options.Err` and does not print usage.

Command trees are intended to be constructed once per process invocation. `Run` is not safe for concurrent use of the same command tree.

## Not In Scope (Core)

The first pass intentionally excludes:
- Shell completions
- Manpage/markdown doc generation
- Help template engines / extensive help customization hooks
- Global/package-level behavior toggles
- Process-exiting helpers (library APIs must not `os.Exit`)

## Public Interface

```go {api}
package cli

type Options struct {
	// Args is the argv excluding the program name (typically os.Args[1:]).
	Args []string

	// In/Out/Err override standard I/O. If nil, defaults are used.
	In  io.Reader
	Out io.Writer
	Err io.Writer
}
```

```go {api}
// Context is passed to a command handler.
//
// Positional args are in Args. Flag values are typically read via variables bound at command construction time (e.g. fs.Bool(...)).
type Context struct {
	context.Context
	Command *Command
	Args    []string
	In      io.Reader
	Out     io.Writer
	Err     io.Writer
}
```

```go {api}
// RunFunc is a command handler.
type RunFunc func(c *Context) error

// ArgsFunc validates positional args. It should return a UsageError (or any ExitCoder with code 2) for user-facing usage mistakes.
type ArgsFunc func(args []string) error
```

```go {api}
// Command defines one CLI command in a command tree.
type Command struct {
	Name    string   // Name is the token used to invoke this command (e.g. "add" in "doc add").
	Aliases []string // Aliases are additional tokens that invoke this command.
	Hidden  bool     // Hidden hides this command from parent help listings, but it may still be invoked normally by name or alias.
	Short   string
	Long    string
	Example string
	Args    ArgsFunc // optional
	Run     RunFunc  // optional

	// ...
}

// AddCommand adds child commands under c.
func (c *Command) AddCommand(children ...*Command)

// Commands returns the direct children of c.
func (c *Command) Commands() []*Command

// Flags returns c's local flags.
func (c *Command) Flags() *FlagSet

// PersistentFlags returns flags inherited by c and its descendants.
func (c *Command) PersistentFlags() *FlagSet
```

```go {api}
// FlagSet is a typed flag registry for a command.
type FlagSet struct {
	// ...
}

func (fs *FlagSet) Bool(name string, shorthand rune, def bool, usage string) *bool
func (fs *FlagSet) String(name string, shorthand rune, def string, usage string) *string
func (fs *FlagSet) Int(name string, shorthand rune, def int, usage string) *int
func (fs *FlagSet) Duration(name string, shorthand rune, def time.Duration, usage string) *time.Duration
```

```go {api}
// Run executes a command tree as a CLI program and returns a process exit code.
func Run(ctx context.Context, root *Command, opts Options) int
```

```go {api}
// ExitCoder is an error with an explicit process exit code.
type ExitCoder interface {
	error
	ExitCode() int
}

// UsageError indicates a user-facing mistake (exit code 2).
type UsageError struct {
	Message string
}

func (e UsageError) Error() string
func (e UsageError) ExitCode() int

// ExitError wraps an error with a specific exit code.
type ExitError struct {
	Code int
	Err  error
}

func (e ExitError) Error() string
func (e ExitError) Unwrap() error
func (e ExitError) ExitCode() int
```

```go {api}
// Args helpers.
func NoArgs(args []string) error
func ExactArgs(n int) ArgsFunc
func MinimumArgs(n int) ArgsFunc
func RangeArgs(min, max int) ArgsFunc
```
