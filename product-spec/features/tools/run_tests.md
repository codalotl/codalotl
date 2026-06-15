# `run_tests`

`run_tests` runs package tests and package verification lints.

## Inputs

- `path`: package directory path, absolute or sandbox-relative.
- `test_name`: optional test name or pattern to pass to `go test -run`.
- `verbose`: optional boolean; when true, runs tests with verbose output.
- `env`: optional environment variable assignments for the test command, like `MYVAR=1 OTHERVAR=2`.

## Output

The tool returns the package test result and any configured lint-check result.

Errors include invalid parameters, missing or non-directory paths, denied permissions, invalid environment assignments, command execution failures, test failures, and lint failures.

Example output:

```text
<test-status ok="true">
$ go test ./internal/lints
ok  	github.com/codalotl/codalotl/internal/lints	(cached)
</test-status>
<lint-status ok="true">
<command ok="true" message="no issues found" mode="check">
$ gofmt -l internal/lints
</command>
<command ok="true" message="no issues found" mode="check">
$ codalotl spec diff internal/lints
</command>
<command ok="true" message="no issues found" mode="check" instructions="never manually fix these unless asked; fixing is automatic on apply_patch">
$ codalotl docs reflow --check internal/lints
</command>
<command ok="true" message="no issues found" mode="check">
$ staticcheck ./internal/lints
</command>
</lint-status>
```

## Behavior

- The agent supplies one Go package path to test.
- Relative paths are resolved from the sandbox dir.
- The path must resolve to a directory.
- The tool runs `go test` for the selected package path.
- The agent can run one named test or test pattern when it needs focused feedback.
- The agent can request verbose test output when debugging failures.
- The agent can provide environment variable assignments for tests that require opt-in settings or fixtures.
- After the test run, the tool runs configured lint checks for package test verification.

## Presentation

Example display:

```text
• Ran Tests path/to/pkg
  └ Tests: pass | Lints: pass
```

If tests or lints fail, the presentation should still stay compact and show the status summary when available:

```text
• Ran Tests path/to/pkg
  └ Tests: fail | Lints: pass
```
