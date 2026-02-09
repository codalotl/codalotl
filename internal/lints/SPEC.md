# lints

The `lints` package implements an extensible "lint pipeline", which is just a list of linters that are run, usually by executing shell commands. For example, we may have the following linters:
- `gofmt`
- `codalotl docs reflow`
- `staticcheck`
- `golangci-lint`

These linters can sometimes fix problems, and sometimes can only detect them (depending on the capabilities of the linter). We support running them in `fix` or `check`
modes. A linter that doesn't support `fix` will still be run in `fix` mode - it will simply report problems without fixing them.

These linters may be run in the following situations (situations are UX contexts):
- During initial context creation (see `initialcontext`) - only checking, no fixing.
- Auto fixing after applying a patch (see `apply_patch` tool).
- As a dedicated fix action (see `fix_lints` tool).
- As a dedicated check action (does not map to an existing `codalotl` tool as of 2026-02-09).

Situations are used to selectively enable lints on a lint-by-lint basis to control the desired developer experience. For example, some lints are noisy and we may not want them run all the time. Others are expensive and we want to avoid them running during initial context creation. Others may automatically apply invasive refactors, and aren't appropriate to apply during a patch.

Situation imply to an action (`check` vs `fix`):
- `initial` and `check` imply action `check`.
- `patch` and `fix` imply action `fix`.

Situations can also be used to enable/disable individual steps.

## Output

This package runs all shell commands via `internal/q/cmdrunner`, and reports status to LLMs via the `ToXML` method (with `lint-status` element).

The `ok` attribute is handled as such:
- In `check` mode, `ok="false"` -> lint issues found (or a command caused an error of some kind). `ok="true"` means there were no linting issues.
- In `fix` mode, if all issues were successfully fixed, `ok="true"`; `ok="false"` is used if a command caused an error, or could not be fixed (including when a lint has no fix capability).

In `check` mode, each `command` element will have a `mode="check"` attribute. In `fix` mode, all commands that support fixing will have `mode="fix"`; those that don't support auto-fixing will have `mode="check"`.

Example output:

```xml
<lint-status ok="false">
<command ok="true" mode="check" message="no issues found">
$ gofmt -l ./internal/q/cmdrunner
</command>
<command ok="false" mode="check">
$ golangci-lint run ./internal/q/cmdrunner
Found 2 issues:
- issue1
- issue2
</command>
</lint-status>
```

To help LLMs understand the meaning of `ok="true|false"`, `command` elements may include `message="no issues found"` (this varies by particular lint). Example:

```xml
<command ok="true" mode="fix" message="no issues found">
$ codalotl docs reflow --width=120 path/to/pkg
</command>
```

Similarly, `command` elements may have custom attributes to help guide the LLM. Ex: `<command ok="false" mode="fix" note="please ignore error 1023">`.

If there are no steps, the output is:

`<lint-status ok="true" message="no linters"></lint-status>`

## Config

The `Lints` config struct can be loaded with JSON as part of a broader config file. For example, a `Config` like the following can load the JSON below:

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
          "args": ["{{ .relativePackageDir }}"],
          "cwd": "{{ .moduleDir }}"
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
- Duplicate step IDs are an error, but only for steps whose ID is set.
- In `extend` mode, "duplicate" includes collisions with default steps.
- IDs listed in `disable` that don't match any resolved step ID are ignored.

This yields:
- Add a lint: append a step to `steps`.
- Disable a lint: add its `id` to `disable` (only affects steps with a set ID).
- Override a default lint: use `mode:"replace"` and provide the full desired set.
- Disable all lints: use `mode:"replace"` with an empty `steps` list.

Reserved/default step IDs:
- `gofmt`
- `reflow`

### Templating

The commands are specified and run with `internal/q/cmdrunner`. As such, they use its template variables:
- `rootDir` = sandbox dir
- inputs:
  - `path` (`InputTypePathDir`): absolute package directory.
  - `moduleDir` (`InputTypePathDir`): absolute module directory (dir of `go.mod`).
  - `relativePackageDir` (`InputTypeString`): package dir relative to `moduleDir` (ex: `internal/somepkg`).
- cmdrunner templating is available (ex: `manifestDir`, `relativeTo`, `repoDir`, `DevNull`).

## Default Lints

By default, only `gofmt` is active. The `reflow` lint (or any other preconfigured lint) is easily available by extending with `"steps": [{"id": "reflow"}]`.

### gofmt

The following code is an example of how gofmt is run (this code is for illustration purposes and does not need to exist as-is). Note how `check` fails on any output while `fix` writes files and does not fail just because it printed the changed file list. `gofmt` is run in all situations.

```go
func newGoFmtRunner(fix bool) *cmdrunner.Runner {
	inputSchema := map[string]cmdrunner.InputType{
		"path":               cmdrunner.InputTypePathDir,
		"moduleDir":          cmdrunner.InputTypePathDir,
		"relativePackageDir": cmdrunner.InputTypeString,
	}
	runner := cmdrunner.NewRunner(inputSchema, []string{"path", "moduleDir", "relativePackageDir"})
	args := []string{"-l"}
	attrs := []string{"mode"}
	if fix {
		args = append(args, "-w")
		attrs = append(attrs, "fix")
	} else {
		attrs = append(attrs, "check")
	}
	args = append(args, "{{ .relativePackageDir }}")

	runner.AddCommand(cmdrunner.Command{
		Command:                "gofmt",
		Args:                   args,
		OutcomeFailIfAnyOutput: !fix,
		MessageIfNoOutput:      "no issues found",
		CWD:                    "{{ .moduleDir }}",
		Attrs:                  attrs,
	})
	return runner
}
```

### Special-case: `codalotl docs reflow`

Any step whose `ID` is `reflow` is executed in-process:
- Calls `updatedocs.ReflowDocumentationPaths` with the package path.
- Extracts the width from a `--width=N` or `--width N` argument in `Args`.
- `reflow` is NOT enabled in `SituationInitial`.

It is rendered as a cmdrunner-like command result so the lint output is uniform, but it is not actually executed as a subprocess. The result lists modified files. Example (in fix mode):

```
<command ok="true" mode="fix">
$ codalotl docs reflow path/to/pkg
path/to/pkg/file1.go
path/to/pkg/file2.go
</command>
```

or

```
<command ok="true" mode="fix" message="no issues found">
$ codalotl docs reflow path/to/pkg
</command>
```

Note: the `--width=N` is NOT rendered in the output (despite being present in `Args`). This is because the LLM can become fixated on the width and start manually
editing files to be within the width; even if it doesn't, it wastes attention on something that is fully automated.

In `check` mode:
- The same rendering is used, but `ok="false"` when any files would change (and the output lists those files).
- The command invocation is rendered with `--check`.
- Attrs are used to give instructions: `instructions="never manually fix these unless asked; fixing is automatic on apply_patch"` (only for `check`).

## Public API

```go
// Situation indicates the context under which the lints are run.
// Internally, `SituationInitial`/`SituationCheck` map to action `check`, and `SituationPatch`/`SituationFix` map to action `fix`.
type Situation string

const (
	SituationInitial Situation = "initial"
	SituationPatch   Situation = "patch"
	SituationFix     Situation = "fix"
	SituationCheck   Situation = "check"
)

// ConfigMode represents the configuration mode of specifying steps: do we extend existing steps, or replace them all with the given steps?
type ConfigMode string

const (
	ConfigModeExtend  ConfigMode = "extend"
	ConfigModeReplace ConfigMode = "replace"
)

// Lints is the user-configurable lint pipeline. It is intended to live under the top-level `lints` key in config JSON.
type Lints struct {
	Mode    ConfigMode `json:"mode,omitempty"`
	Disable []string   `json:"disable,omitempty"`
	Steps   []Step     `json:"steps,omitempty"`
}

type Step struct {
	ID string `json:"id,omitempty"` // Optional. Empty string means "unset". Multiple steps may have an unset ID.

	// The step will be run in the following situations.
	// - If omitted/null: run in all situations.
	// - If []: run in no situations.
	Situations []Situation `json:"situations,omitempty"`

	// Check/Fix override Cmd for their respective actions.
	Check *cmdrunner.Command `json:"check,omitempty"`
	Fix   *cmdrunner.Command `json:"fix,omitempty"`
}

// DefaultSteps returns default steps. It is equivalent to ResolveSteps(nil, 0).
func DefaultSteps() []Step

// ResolveSteps merges defaults and user config, applying disable rules.
// Validation errors (unknown mode, invalid step definitions, duplicate IDs, etc.) return an error.
// It also normalizes any `codalotl docs reflow` step to include `--width=<reflowWidth>` when missing.
func ResolveSteps(cfg *Lints, reflowWidth int) ([]Step, error)

// Run executes steps for the given situation against targetPkgAbsDir and returns cmdrunner XML (`lint-status`).
//
// - sandboxDir is the cmdrunner rootDir.
// - targetPkgAbsDir is an absolute package directory.
// - Run does not stop early: it attempts to execute all steps, even if earlier steps report failures.
// - Command failures are reflected in the XML. Hard errors (invalid config, templating failures, internal errors) return a Go error.
func Run(ctx context.Context, sandboxDir string, targetPkgAbsDir string, steps []Step, situation Situation) (string, error)
```
