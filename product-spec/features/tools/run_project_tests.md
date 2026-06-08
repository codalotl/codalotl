# `run_project_tests`

`run_project_tests` lets an agent run the broader Go project test suite when it needs confidence beyond the currently selected package.

## Availability

- Available in package-mode agents.
- Available to package agents as a broader project test check when package-level tests are not enough.

## Behavior

- The tool runs the repository's project-wide Go tests, equivalent to `go test ./...`.
- When the agent is working in package mode, the tests run from the Go module containing the selected package.
- If no package path is available, the tests run from the sandbox dir.
- The tool is intended for use after the selected package's own tests pass.
- The tool returns project test status and enough output to identify failures.

## Inputs

This tool has no agent-supplied inputs.

## Output

The tool returns a test status result for the project-wide test run.

On success, the result indicates that project tests passed. On failure, the result includes failing package or test lines when they can be extracted from the test output.

Errors include invalid project paths, missing or unusable Go module context, command execution failures, and test failures.

## Presentation

Human-facing output presents the project test run as:

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
