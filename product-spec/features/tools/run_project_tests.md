# `run_project_tests`

`run_project_tests` runs the broader Go project test suite.

## Inputs

This tool has no agent-supplied inputs.

## Output

The tool returns a test status result for the project-wide test run.

On success, the result indicates that project tests passed. On failure, the result includes failing package or test lines when they can be extracted from the test output.

Errors include invalid project paths, missing or unusable Go module context, command execution failures, and test failures.

## Behavior

- The tool runs the repository's project-wide Go tests, equivalent to `go test ./...`.
- In package mode, tests run from the Go module containing the selected package.
- If no package path is available, tests run from the sandbox dir.
- The tool is intended for use after the selected package's own tests pass.
- The tool returns enough output to identify project test failures.

## Presentation

Example display:

```text
• Ran Tests ./...
  └ Passed
```

If the run fails, the presentation should show a compact failure summary:

```text
• Ran Tests ./...
  └ Failed:
    github.com/codalotl/codalotl/internal/example
```

The presentation should not dump the full `go test ./...` output when failing package or test lines can communicate the result clearly.

## Permissions

The project test base path is authorized before tests run.

In package mode, `run_project_tests` gives the package agent a project-level verification lever without granting a general-purpose shell.
