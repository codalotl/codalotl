# `fix_lints`

`fix_lints` lets a package-mode agent apply configured lint fixes to a Go package path.

It gives the agent a dedicated way to run automatic cleanup, such as `gofmt` or project-specific documentation/spec formatting, without granting a general-purpose shell.

## Availability

- Available in package-mode agents.
- Available to package agents when they need to explicitly fix lint issues for the selected package path.

## Behavior

- The agent supplies one package directory path.
- Relative paths are resolved from the sandbox dir.
- The path must resolve to an existing directory.
- The tool authorizes writes to the target path before running lint fixes.
- The tool runs the configured lint pipeline in the dedicated lint-fix situation.
- Lint steps that support fixing may edit files. Steps that only support checking may still report remaining issues.
- If no lint steps are configured, the tool reports a successful no-linters status.
- On success, the tool returns lint status output showing which commands ran and whether lint issues were fixed or remain.
- On command failure, unfixable lint issue, authorization failure, invalid path, or invalid parameters, the tool returns an error.

## Inputs

- `path`: package directory path, absolute or sandbox-relative.

## Output

The tool returns the lint pipeline result for the requested path.

In fix mode, a successful result means all enabled lint steps either found no issues or fixed the issues they are able to fix. A failure may mean a command failed, or that a check-only lint found issues that cannot be fixed automatically.

The agent-facing output may include structured lint status details, including command names, check/fix mode, status, changed files, and remaining issue output.

## Presentation

Human-facing output presents the operation as:

```text
• Fixed Lints internal/example
```

When there is useful lint output, the presentation may include a compact summary:

```text
• Fixed Lints internal/example
  └ $ gofmt -w internal/example
    internal/example/foo.go
```

The presentation should summarize output rather than dump the full structured lint status. Wrapper tags such as `lint-status` and `command` are not shown in the human-facing body.

## Permissions

Writes are authorized before lint fixes run.

In package mode, `fix_lints` gives the agent a package-aware cleanup tool that can apply configured mechanical fixes while preserving the selected package boundary.
