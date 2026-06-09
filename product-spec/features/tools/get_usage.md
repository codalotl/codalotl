# `get_usage`

`get_usage` inspects code that references a package-defined Go identifier.

## Inputs

- `defining_package_path`: Go package directory relative to the sandbox, or a Go import path.
- `identifier`: identifier defined in `defining_package_path`.

## Output

The tool returns an agent-facing usage summary with references to packages, files, line numbers, source lines, and selected code snippets when helpful.

Errors include invalid parameters, unresolved packages, package load failures, authorization failures for sandbox packages, and identifiers that are not defined by the target package.

## Behavior

- The agent supplies the Go package that defines the identifier.
- The defining package may be a sandbox-relative package directory or a Go import path.
- The agent supplies one identifier defined by that package.
- Identifier forms include package-level functions, types, vars, consts, and methods such as `T.M` or `*T.M`.
- The tool resolves and loads the defining package, then finds references to that identifier from packages that use it.
- The result focuses on packages and files that use the selected or defining package, so the agent can reason about callers without broadly reading unrelated source.
- The result may include intra-package references when that helps explain how the identifier is used.
- When downstream code must change, the agent should use `update_usage` rather than directly editing downstream packages from the main package-mode session.

## Presentation

Example display:

```text
• Read Usage path/or/import/pkg Identifier
  └ Found 2 results.
```

## Permissions

Reads of packages inside the sandbox are authorized before usage information is generated.

Packages outside the sandbox that are resolved through Go's standard library or module dependency graph may be read as dependency context. The tool does not grant write access to those packages.
