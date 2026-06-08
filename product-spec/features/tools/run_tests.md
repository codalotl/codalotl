# `run_tests`

`run_tests` lets a package-mode agent run selected package tests and lint checks as part of ordinary package work.

## Availability

- Available in package-mode agents.
- Available to delegated package agents when their toolset includes package verification.

## Behavior

- The agent supplies one Go package path to test.
- Relative paths are resolved from the sandbox dir.
- The path must resolve to a directory.
- The tool runs `go test` for the selected package path.
- The agent can run one named test or test pattern when it needs focused feedback.
- The agent can request verbose test output when debugging failures.
- The agent can provide environment variable assignments for tests that require opt-in settings or fixtures.
- After the test run, the tool runs configured lint checks for package test verification.
- The tool returns enough test and lint output for the agent to understand whether the package is passing and what failed.

## Inputs

- `path`: package directory path, absolute or sandbox-relative.
- `test_name`: optional test name or pattern to pass to `go test -run`.
- `verbose`: optional boolean; when true, runs tests with verbose output.
- `env`: optional environment variable assignments for the test command, like `MYVAR=1 OTHERVAR=2`.

## Output

The tool returns the package test result and the configured lint-check result.

When possible, output separates test status from lint status so the agent can tell whether a failure came from tests, lint checks, or both. More detailed command output may be included when needed to diagnose failures.

Errors include invalid parameters, missing or non-directory paths, denied permissions, invalid environment assignments, command execution failures, test failures, and lint failures.

## Presentation

Human-facing output presents a package test run as:

```text
• Ran Tests path/to/pkg
  └ Tests: pass | Lints: pass
```

If tests or lints fail, the presentation should still stay compact and show the status summary when available:

```text
• Ran Tests path/to/pkg
  └ Tests: fail | Lints: pass
```

When status sections are unavailable, the presentation may show a short summary of command output instead of dumping the full `go test` or lint output.

## Permissions

The package path is authorized before tests and test-time lint checks run.

In package mode, `run_tests` gives the agent a package-scoped verification tool without granting a general-purpose shell.
