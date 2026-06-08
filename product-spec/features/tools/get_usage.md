# `get_usage`

`get_usage` lets a package-mode agent inspect code that references a package-defined Go identifier.

It is the preferred way for an agent to understand downstream callers before changing behavior, signatures, exported types, or other identifiers that other packages may depend on.

## Availability

- Available in package-mode agents.
- Available to package-aware delegated agents when their toolset includes usage inspection.
- Not a generic filesystem search tool.

## Behavior

- The agent supplies the Go package that defines the identifier.
- The defining package may be a sandbox-relative package directory or a Go import path.
- The agent supplies one identifier defined by that package.
- Identifier forms include package-level functions, types, vars, consts, and methods such as `T.M` or `*T.M`.
- The tool resolves and loads the defining package, then finds references to that identifier from packages that use it.
- The result should focus on packages and files that use the selected or defining package, so the agent can reason about callers without broadly reading unrelated source.
- The result may include intra-package references when that helps explain how the identifier is used, but usage from the defining package's own external test package may be excluded.
- The tool is for read-only impact analysis. When downstream code must change, the agent should use `update_usage` rather than editing those packages directly from the main package-mode session.

## Inputs

- `defining_package_path`: Go package directory relative to the sandbox, or a Go import path.
- `identifier`: identifier defined in `defining_package_path`.

## Output

The tool returns an agent-facing usage summary with references to packages, files, line numbers, source lines, and selected code snippets when helpful.

The output is intentionally usage-oriented rather than a raw repository search dump. It should preserve enough caller context for the agent to understand how the identifier is used and what could break if it changes.

Errors include invalid parameters, unresolved packages, package load failures, authorization failures for sandbox packages, and identifiers that are not defined by the target package.

## Presentation

Human-facing output presents a successful usage inspection as:

```text
• Read Usage path/or/import/pkg Identifier
  └ Found N results.
```

For a single usage result, the body uses the singular form:

```text
• Read Usage path/or/import/pkg Identifier
  └ Found 1 result.
```

The presentation should not dump the returned usage summary into the progress line. The references and snippets belong to the agent-facing result.

## Permissions

Reads of packages inside the sandbox are authorized before usage information is generated.

Packages outside the sandbox that are resolved through Go's standard library or module dependency graph may be read as dependency context. The tool does not grant write access to those packages.
