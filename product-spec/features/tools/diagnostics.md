# `diagnostics`

`diagnostics` inspects Go build and type-check diagnostics for a package.

## Inputs

- `path`: package path, absolute or sandbox-relative.

## Output

The tool returns diagnostic status for the requested package, including enough build or type-check output for the agent to identify the failing files, lines, and messages when available.

Errors include invalid parameters, missing paths, non-directory paths, denied permissions, missing or unusable Go module context, command execution failures, and diagnostics runner failures.

Example output:

```text
<diagnostics-status ok="true" message="build succeeded">
$ go build -o /dev/null ./pricing
</diagnostics-status>
```

## Behavior

- The agent supplies one package path.
- Relative paths are resolved from the sandbox dir.
- The path must resolve to a Go package directory or package-like directory.
- The tool runs Go diagnostics for that package, roughly equivalent to building the package without producing a user-visible binary.
- Build and type-check failures are returned as diagnostic status for the agent to act on.
- A package with no diagnostics returns a successful diagnostic status rather than an empty or missing result.

## Presentation

Example display:

```text
• Ran Diagnostics path/to/package
```
