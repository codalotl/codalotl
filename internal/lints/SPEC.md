# lints

The `lints` package implements an extensible "lint pipeline", which is just a list of linters that are run, usually by executing shell commands. For example, we may have the following linters:
- `gofmt`
- `codalotl docs reflow`
- `staticcheck`
- `golangci-lint`

These linters sometimes can fix problems, and sometimes can only detect them (depending on the capabilities of the linter). We support running them in `fix` or `check` modes. A linter that doesn't support `fix` will still be run in `fix` mode - it will simply report problems without fixing them.

These linters may be run in the following contexts within `codalotl`:
- After `apply_patch`; during the `fix_lints` tool; as part of automatic context creation (see `initialcontext`).

## Output

This package runs all shell commands via `internal/q/cmdrunner`, and reports status to LLMs via the `ToXML` method (with `lint-status` tag).
- In `check` mode, `ok="false"` -> lint issues found (or a command caused an error of some kind). `ok="true"` means there were no linting issues.
- In `fix` mode, if all issues were successfully fixed, `ok="true"`; `ok="false"` is used if a command caused an error, or could not be fixed (including when a lint has no fix capability).

Example output:

```xml
<lint-status ok="false">
<command ok="true" message="no issues found">
$ gofmt -l ./internal/q/cmdrunner
</command>
<command ok="false">
$ golangci-lint run ./internal/q/cmdrunner
Found 2 issues:
- issue1
- issue2
</command>
</lint-status>
```

To help LLMs understand the meaning of `ok="true|false"`, `command` elements may include `message="no issues found"`. Example:

```xml
<command ok="true" message="no issues found">
$ codalotl docs reflow --width=120 path/to/pkg
</command>
```

If there are no steps, the output is:

`<lint-status ok="true"></lint-status>`

## Config

The `Lints` config struct can be loaded with JSON as part of a broader config file. For example, a `Config` like the following can load the subsequent JSON:


```go
type Config struct {
	// ... other config ...
	ReflowWidth int         `json:"reflowwidth"`
	Lints       lints.Lints `json:"lints,omitempty"`
}
```

```json
{
  "lints": {
    "mode": "extend",
    "steps": [
      {
        "id": "staticcheck",
        "check": {
          "command": "staticcheck",
          "args": ["{{ .path }}"],
          "cwd": "{{ manifestDir .path }}"
        }
      }
    ]
  }
}
```

### UX (override vs extend)

Rules:
- If the `lints` object is missing entirely: run defaults.
- If `lints.mode` is missing/empty: treat as `extend`.
- In `extend` mode, duplicate step IDs are an error (including collisions with defaults).

This yields:
- Add a lint: append a step to `steps`.
- Disable a lint: add its `id` to `disable`.
- Override a default lint: use `mode:"replace"` and provide the full desired set.
- Disable all lints: use `mode:"replace"` with an empty `steps` list.

Reserved/default step IDs:
- `gofmt`
- `reflow`

### Templating

The commands are specified and run with `internal/q/cmdrunner`. As such, they use its template variables:
- `rootDir` = sandbox dir
- inputs:
  - `path`: absolute package directory (cmdrunner `InputTypePathDir`)
- cmdrunner templating is available (ex: `manifestDir`, `relativeTo`, `repoDir`, `DevNull`).

## Default Lints

By default, `gofmt` and `codalotl docs reflow` are the two installed linters.

### gofmt

The following code is an example of how gofmt is run (this code is for illustration purposes and does not need to exist as-is). Note how `check` fails on any output while `fix` writes files and does not fail just because it printed the changed file list.

```go
func newGoFmtRunner(fix bool) *cmdrunner.Runner {
	inputSchema := map[string]cmdrunner.InputType{
		"path": cmdrunner.InputTypePathDir,
	}
	runner := cmdrunner.NewRunner(inputSchema, []string{"path"})
	args := []string{"-l"}
	if fix {
		args = append(args, "-w")
	}
	args = append(args, "{{ relativeTo .path (manifestDir .path) }}")
	runner.AddCommand(cmdrunner.Command{
		Command:                "gofmt",
		Args:                   args,
		OutcomeFailIfAnyOutput: !fix,
		MessageIfNoOutput:      "no issues found",
		CWD:                    "{{ manifestDir .path }}",
	})
	return runner
}
```

### Special-case: `codalotl docs reflow`

Any step whose `ID` is `reflow` is executed in-process:
- Calls `updatedocs.ReflowDocumentationPaths` with the package path.
- Extracts the width from a `--width=N` or `--width N` argument (from the command selected for the current action).

It is rendered as a cmdrunner-like command result so the lint output is uniform, but it is not actually executed as a subprocess. The result lists modified files. Example (in fix mode):

```
<command ok="true">
$ codalotl docs reflow --width=120 path/to/pkg
path/to/pkg/file1.go
path/to/pkg/file2.go
</command>
```

or

```
<command ok="true" message="no issues found">
$ codalotl docs reflow --width=120 path/to/pkg
</command>
```

In `check`, the same rendering is used, but `ok="false"` when any files would change (and the output lists those files).

## Public API

```go
type Action string

const (
	ActionCheck Action = "check"
	ActionFix   Action = "fix"
)

type Mode string

const (
	ModeExtend  Mode = "extend"
	ModeReplace Mode = "replace"
)

// Lints is the user-configurable lint pipeline. It is intended to live under the top-level `lints` key in config JSON.
type Lints struct {
	Mode    Mode     `json:"mode,omitempty"`
	Disable []string `json:"disable,omitempty"`
	Steps   []Step   `json:"steps,omitempty"`
}

type Step struct {
	ID  string   `json:"id,omitempty"`

	// Check/Fix override Cmd for their respective actions.
	Check *cmdrunner.Command `json:"check,omitempty"` // TODO: tag cmdrunner.Command with JSON tags
	Fix   *cmdrunner.Command `json:"fix,omitempty"`
}

// ResolveSteps merges defaults and user config, applying disable rules.
// Validation errors (unknown mode, invalid step definitions, duplicate IDs, etc.) return an error.
// It also normalizes any `codalotl docs reflow` step to include `--width=<reflowWidth>` when missing.
func ResolveSteps(cfg *Lints, reflowWidth int) ([]Step, error)

// Run executes steps for the given action against targetPkgAbsDir and returns cmdrunner XML (`lint-status`).
//
// - sandboxDir is the cmdrunner rootDir.
// - targetPkgAbsDir is an absolute package directory.
// - Command failures are reflected in the XML. Hard errors (invalid config, templating failures, internal errors) return a Go error.
func Run(ctx context.Context, sandboxDir string, targetPkgAbsDir string, steps []Step, action Action) (string, error)
```
