# `diagnostics`

`diagnostics` lets a package-mode agent inspect Go build and type-check diagnostics for a package path.

## Availability

- Available in package-mode agents.
- Available to delegated package agents when they need to verify or investigate the selected package.

## Behavior

- The agent supplies one package path.
- Relative paths are resolved from the sandbox dir.
- The path must resolve to a Go package directory or package-like directory inside the current authorization boundary.
- The tool runs Go diagnostics for that package, roughly equivalent to building the package without producing a user-visible binary.
- Build and type-check failures are returned as diagnostic status for the agent to act on.
- A package with no diagnostics returns a successful diagnostic status rather than an empty or missing result.

## Inputs

- `path`: package path, absolute or sandbox-relative.

## Output

The tool returns diagnostic status for the requested package, including enough build or type-check output for the agent to identify the failing files, lines, and messages when available.

Errors include invalid parameters, missing paths, non-directory paths, denied permissions, missing or unusable Go module context, command execution failures, and diagnostics runner failures.

## Presentation

Human-facing output presents the diagnostic run as:

```text
• Ran Diagnostics path/to/package
```

While the tool is still running, human-facing output may present:

```text
• Run Diagnostics path/to/package
```

If the run itself fails, the presentation should show a compact error owned by the diagnostics presenter rather than dumping raw tool JSON.

## Permissions

Diagnostic reads are authorized before the Go diagnostic command runs.

In package mode, `diagnostics` reinforces the selected package boundary: the agent can directly inspect diagnostics for the selected package code unit, while diagnostics for other locations require authorization or a package-aware workflow that grants access.
