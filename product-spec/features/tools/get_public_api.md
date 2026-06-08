# `get_public_api`

`get_public_api` lets a package-mode agent inspect the exported API of another Go package without reading that package's source files directly.

It is the preferred way for an agent to understand how to use an imported package from the selected package.

## Availability

- Available in package-mode agents.
- Available to package-aware delegated agents when their toolset includes public API inspection.
- Not a generic filesystem read tool.

## Behavior

- The agent supplies one Go package target.
- The target may be a sandbox-relative package directory or a Go import path.
- Import paths may refer to packages in the current module, direct or transitive module dependencies available to Go tooling, or the Go standard library.
- The tool resolves and loads the target package, then returns concise godoc-style documentation for exported identifiers.
- Returned documentation includes public declarations, documentation comments when present, and signatures needed to call the API.
- The result excludes package-private declarations and function bodies.
- The agent may request specific identifiers to narrow the output.
- When a requested identifier is an exported type, the output should include that type's public methods.
- Method identifiers may use Go-style forms such as `T.M` or `*T.M`.
- The tool is for understanding how to use another package. When the public API docs are insufficient, the agent should use `clarify_public_api` rather than falling back to broad source reads.

## Inputs

- `path`: Go package directory relative to the sandbox, or a Go import path.
- `identifiers`: optional list of exported identifiers or methods to fetch docs for.

## Output

The tool returns compact public package documentation suitable for agent context.

The output is intentionally documentation-oriented rather than source-oriented. It should preserve enough package, file, declaration, and signature context for the agent to use the API correctly, while keeping implementation details out of the result.

Errors include invalid parameters, unresolved packages, package load failures, authorization failures for sandbox packages, test-package targets when unsupported, and unknown or invalid requested identifiers.

## Presentation

Human-facing output presents a successful API inspection as:

```text
• Read Public API path/or/import/pkg
```

When identifiers are requested, the presentation may include them as a compact body:

```text
• Read Public API path/or/import/pkg
  └ TypeName, FuncName, *TypeName.MethodName
```

The presentation should not dump the returned documentation into the progress line. The documentation belongs to the agent-facing result.

## Permissions

Reads of packages inside the sandbox are authorized before package documentation is generated.

Packages outside the sandbox that are resolved through Go's standard library or module dependency graph may be read as dependency context. The tool does not grant write access to those packages.
