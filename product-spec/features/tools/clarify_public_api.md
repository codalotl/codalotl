# `clarify_public_api`

`clarify_public_api` asks a focused read-only agent a specific question about a Go package's public API.

It is for cases where `get_public_api` is not enough and the agent needs a grounded explanation of an identifier before changing the selected package.

## Inputs

- `path`: Go package directory or import path.
- `identifier`: public API identifier needing clarification.
- `question`: specific question for the clarification subagent.

## Output

The tool returns the clarification answer written by the read-only subagent.

Errors include invalid parameters, unresolved package paths or import paths, denied target-package reads, subagent setup or invocation failures, and clarify CAS recording failures.

## Behavior

- The agent supplies a package path, an identifier, and a specific question.
- The package path may be a sandbox-relative package directory or a Go import path.
- The target package may be in the sandbox, in a dependency module, or in the Go standard library.
- The target identifier names the public API symbol being clarified, such as a function, type, variable, constant, method, or pointer-receiver method.
- The caller should read existing public API documentation before asking for clarification.
- The tool starts a read-only subagent scoped to the resolved target package.
- The subagent answers using target package context and read-only tools.
- The subagent should distinguish documented behavior, implementation facts, and unknowns instead of guessing beyond available evidence.
- The clarification answer is returned as the completed tool result rather than appearing as a separate chat message.
- Successful clarifications of sandbox packages may be recorded in clarify CAS for later documentation improvement.

## Presentation

Example display while running:

```text
• Clarifying API SomeIdentifier in some/pkg
  └ What does this option control?
```

Example display after completion:

```text
• Clarified API SomeIdentifier in some/pkg
  └ The option controls ...
```

## Permissions

Sandbox-package reads are authorized before the clarification subagent runs.

The clarification subagent is read-only and scoped to the resolved target package. For dependency and standard-library packages, the tool may run the subagent with the dependency or standard-library root as its effective sandbox so ordinary reads remain confined to the target code unit.

Clarify CAS writes are authorized separately from target-package reads.
