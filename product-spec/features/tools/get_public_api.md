# `get_public_api`

`get_public_api` inspects the exported API of a Go package without exposing its implementation.

## Inputs

- `path`: Go package directory relative to the sandbox, or a Go import path.
- `identifiers`: optional list of exported identifiers or methods to fetch docs for.

## Output

The tool returns compact public package documentation suitable for agent context.

The output is intentionally documentation-oriented rather than source-oriented. It should preserve enough package, file, declaration, and signature context for the agent to use the API correctly, while keeping implementation details out of the result.

Errors include invalid parameters, unresolved packages, package load failures, authorization failures for sandbox packages, test-package targets when unsupported, and unknown or invalid requested identifiers.

## Behavior

- The agent supplies one Go package target.
- The target may be a sandbox-relative package directory or a Go import path.
- Import paths may refer to packages in the current module, dependencies available to Go tooling, or the Go standard library.
- The tool resolves and loads the target package, then returns concise godoc-style documentation for exported identifiers.
- Returned documentation includes public declarations, documentation comments when present, and signatures needed to call the API.
- The result excludes package-private declarations and function bodies.
- The agent may request specific identifiers to narrow the output.
- When a requested identifier is an exported type, the output includes that type's public methods.
- Method identifiers may use Go-style forms such as `T.M` or `*T.M`.
- When public API docs are insufficient, the agent should use `clarify_public_api` rather than broad source reads.

## Presentation

Example display:

```text
• Read Public API path/or/import/pkg
```

When identifiers are requested, the presentation may include them as a compact body:

```text
• Read Public API path/or/import/pkg
  └ TypeName, FuncName, *TypeName.MethodName
```

The presentation should not dump the returned documentation into the progress line.

## Permissions

Reads of packages inside the sandbox are authorized before package documentation is generated.

Packages outside the sandbox that are resolved through Go's standard library or module dependency graph may be read as dependency context. The tool does not grant write access to those packages.
