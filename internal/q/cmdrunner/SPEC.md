# cmdrunner

The cmdrunner package allows consumers to create a configuration of how to execute shell commands. Based on the configuration and inputs, it runs the commands, giving structured access to outputs.

One use-case is if we want a command to "run tests", but on a per-language basis. End-users will often want to override the default test runner with their own specific scripts to run tests. For the end-user use case, they'd want a way to edit a yaml file, indicating how to run tests and how to detect success/failure (actual parsing of config files, and mapping to cmdrunner structs, is outside the scope of this package, for now).

This package also provides a way to chain multiple commands together. For instance, the user may have 5 ways to detect lint errors, each running a different command.

Finally, in addition to structured access to outputs, this package provides a way for LLMs to easily consume tool output by uniformely rendering the command, output, and statuses in an xml tag.

## Usage

Example usage, which configures a `go test` runner for use in Go projects:

```go
// Construct a Runner:
inputSchema := map[string]InputType{"path": InputTypePathDir, "verbose": InputTypeBool, "namePattern": InputTypeString} // path is main input: a filesystem path to the package.
requiredInputs := []string{"path"}
cs := NewRunner(inputSchema, requiredInputs)
cs.AddCommand(Command{
    Command: "go",
    Args: []string{
        "test",
        "{{ if eq .path (manifestDir .path) }}.{{ else }}./{{ relativeTo .path (manifestDir .path) }}{{ end }}",
        "{{ if .verbose }}-v{{ end }}",
        "{{ if ne .namePattern "" }}-run={{ .namePattern }}{{ end }}",
    },
    CWD: "{{ manifestDir .path }}",
})

// Run against inputs:
rootDir := getRoot() // the root is an absolute path. All path types in the inputs can be absolute or relative to this root.
inputs := map[string]any{"path": "some/pkg"}
result, err := cs.Run(ctx, rootDir, inputs)
if err != nil {
    return err // returned for mismatch of inputs vs schema or for templating errors.
}
if result.Success() {
    fmt.Println("tests pass")
} else {
    fmt.Println("tests failed, or error when running command")
    r0 := result.Results[0]
    fmt.Println("output:")
    fmt.Println(r0.Output)
    fmt.Printf("ExecStatus: %v ExitStatus: %d Outcome: %v Duration: %v\n", r0.ExecStatus, r0.ExitCode, r0.Outcome, r0.Duration)
}
```

## Templating

- Default inputs: `RootDir`, `DevNull`
- All path input types are normalized to absolute paths automatically by the time they get to the templating stage.
- If an arg in Args is "" after TrimSpace, it is discarded.
- `relativeTo` converts a path relative to another path.
- `manifestDir` function exists - see the manifestDir section.
- `repoDir` accepts a path (file or dir) and walks towards rootDir, returning the first dir that contains a `.git` directory, otherwise the root dir.

### manifestDir

- Accepts a path (file or dir) argument and returns the dir of the closest relevant manifest file.
- Example: in a Go project, walks up to find the nearest `go.mod` file.
- If the inputs to Run contain a `Lang` var, that is the language's manifest file we're looking for.
- Otherwise, detects the language based on what type of files are in the dir of the `path` input (if no files, walks up until it finds some).
- Once the language is "detected", it won't change as it walks up.
- Language detection, and mapping to manifest filename, is based on a map of filename extention to a slice of manifest filenames.
- Ex: {"go": ["go.mod"], "rb": ["Gemfile"], "py": ["pyproject.toml", "requirements.txt"], ...}
- If no match is found, it returns the root dir.

## Output Rendering for LLMs

In addition to programmatic inspection of the Result/CommandResult, this package can render results for LLMs. We try to optimize for minimal, relevant information, with reasonable consistency, and good scanability.

For example, `fmt.Println(result.ToXML("test-status"))` produces:

```txt
<test-status ok="true">
$ go test ./q/cmdrunner -v -run=TestManifestDirWithLangInput
=== RUN   TestManifestDirWithLangInput
--- PASS: TestManifestDirWithLangInput (0.00s)
PASS
ok      axi/q/cmdrunner 0.003s
</test-status>
```

- Even though I use the term "XML", this is not real XML. Nothing in the body of these tags is escaped.
- If the outcome is Success: `ok="true"`; otherwise `ok="false"`
- If the ExecStatus isn't ExecStatusCompleted, we write the exec status as an attribute (e.g., `exec-status="failed_to_start"`).
- If the exit code isn't 0 or 1, we write the exit code (e.g., `exit-code="2"`).
- If the process was terminated by a signal, we write the signal (e.g., `signal="TERM"`).
- If the duration is greater than a threshold (`DurationWarnThreshold`), we write the duration (e.g., `duration="10.2s"`).
- (Typically, we'll just show the `ok` attribute. If `ok="false"`, we may show extra infomation. The duration can show even if `ok="true"`.)
- We print the command after a $ as the first line in the body of the tag.
- If the command has no output, the output is three lines (tag, $ command, end tag).
- If `MessageIfNoOutput` is set non-empty on the command and there's no output, a `message` attribute will be set on the opening tag.

If the runner has multiple commands:
- The outter tag (ex: `<test-status>` above) will only contain the `ok` attribute.
- Each command will go in its own `<command></command>` tag, with all the same attributes and rules outlined above.

Example of mulitple commands:

```txt
<lint-status ok="false">
<command ok="true">
$ gofmt -l ./q/cmdrunner
</command>
<command ok="false">
$ gochecklint -l ./q/cmdrunner
Found 2 issues:
- issue1
- issue2
</command>
</lint-status>
```

## Public API

```go {api}
// InputType represents the type of value that a command expects for a given input key.
type InputType string

// InputType values.
//
// Path types can be absolute or relative to the root. The Any/Dir/File types are checked for existence (or Run will return an error). InputTypePathUnchecked is
// not checked for existence. All paths are converted to absolute paths before being passed to the templates. Paths are allowed to be outside the root. No special
// symlink handling is performed. InputTypePathDir can be a file in the input. If it is, it must exist as a file. But it is then converted to a directory using filepath.Dir().
const (
	InputTypePathAny       InputType = "path_any"
	InputTypePathDir       InputType = "path_dir"
	InputTypePathFile      InputType = "path_file"
	InputTypePathUnchecked InputType = "path_unchecked"
	InputTypeBool          InputType = "bool"
	InputTypeString        InputType = "string"
	InputTypeInt           InputType = "int"
)
```

```go {api}
// Runner coordinates templating and execution for a collection of commands.
type Runner struct {
	inputSchema    map[string]InputType
	requiredInputs []string
	commands       []Command
}

// Run runs all commands. An error is returned if inputs are invalid or don't match the schema, or if templating fails on any command. Any error encountered during
// execing the command is encapsulated in Result.
func (r *Runner) Run(ctx context.Context, rootDir string, inputs map[string]any) (Result, error)

// AddCommand registers the provided command with the Runner.
func (r *Runner) AddCommand(c Command)
```

```go {api}
// A Command is a templated command to run. The Command/Args/CWD fields support templates.
type Command struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	CWD     string   `json:"cwd"` // optional working directory for the command. Defaults to the root dir.

	// If OutcomeFailIfAnyOutput, any output causes the command to have a failed outcome (ex: `gofmt -l` is blank when no-lint-issues and non-blank when lint-issues).
	OutcomeFailIfAnyOutput bool `json:"outcomefailifanyoutput"`

	// If non-empty and the command's Output is empty, will add a `message` attribute to the opening tag in `ToXML` set to this value.
	MessageIfNoOutput string `json:"messageifnooutput"`

	// If ShowCWD, adds a `cwd` attribute to the opening tag in `ToXML`, showing the CWD from which the command was run.
	ShowCWD bool `json:"showcwd"`

	// Attrs are pairs of keys/values that will be added to the corresponding CommandResult's ToXML tag. len(Attrs) must be a multiple of 2. Strings are NOT validated
	// or escaped. For instance, ["dryrun", "true"] renders (for instance) `<command ok="true" dryrun="true">...</command>`.
	//
	// This can be used to communicate metadata to a consuming LLM about the command.
	Attrs []string `json:"attrs"`
}
```

```go {api}
// Result aggregates all command executions performed by Run.
type Result struct {
	Results []CommandResult
}

// Success returns true if all results are OutcomeSuccess.
func (r Result) Success() bool

// ExecStatus captures how process execution concluded.
type ExecStatus string

const (
	ExecStatusCompleted     ExecStatus = "completed"       // process exited (any code)
	ExecStatusFailedToStart ExecStatus = "failed_to_start" // ENOENT, EPERM, bad shebang, cwd missing
	ExecStatusTimedOut      ExecStatus = "timed_out"
	ExecStatusCanceled      ExecStatus = "canceled"
	ExecStatusTerminated    ExecStatus = "terminated" // by signal; see Signal
)

// Outcome is a semantic command-relative status. Examples:
//   - `go build` is OutcomeFailed for build errors.
//   - `gofmt -l` is OutcomeFailed if it produces output (there's unresolved lints).
//   - `ls` produces OutcomeSuccess unless it errors out (ex: `ls doesnt-exist-path`).
//
// By default:
//   - ExecStatusCompleted && exit code 0 -> OutcomeSuccess
//   - !ExecStatusCompleted || exit code != 0 -> OutcomeFailed
//
// The default can be changed with flags on the Command type (ex: OutcomeFailIfAnyOutput).
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailed  Outcome = "failed"
)

// CommandResult captures the execution details for a single command.
type CommandResult struct {
	Command           string   // Command is the actual, rendered command run.
	Args              []string // Args are the actual, rendered args used.
	CWD               string   // CWD is the actual, rendered CWD used.
	Output            string   // The stdout + stderr of the command.
	MessageIfNoOutput string   // If non-empty and Output is empty, will render a `message` attribute in `ToXML` set to this value.
	ShowCWD           bool     // If ShowCWD, adds a `cwd` attribute to the opening tag in `ToXML`, showing the CWD from which the command was run.

	// Attrs are pairs of keys/values that will be added to the corresponding command tag when rendering ToXML output. len(Attrs) must be a multiple of 2. Strings are
	// NOT validated or escaped.
	Attrs []string

	ExecStatus ExecStatus
	ExecError  error // if an error is returned from exec'ing the command, it's set here.
	ExitCode   int
	Signal     string // if the command was terminated due to a signal (ex: "TERM")
	Outcome    Outcome
	Duration   time.Duration
}
```

```go {api}
// ManifestDir returns the manifest dir for `path` and the path relative to that manifest dir.
//
// Conceptually equivalent to:
//   - {{ manifestDir .path }}
//   - {{ relativeTo .path (manifestDir .path) }}
//
// Errors are returned if `rootDir` is empty/invalid/not a directory, or if the relative path computation fails.
func ManifestDir(rootDir string, path string) (string, string, error)
```
