# `clarify_public_api`

`clarify_public_api` lets a package-mode agent ask a focused read-only subagent a specific question about another Go package's public API.

It is for cases where `get_public_api` is not enough and the agent needs a grounded explanation of an identifier before changing the selected package.

## Availability

- Available in package-mode agents.
- Available to delegated package agents when they need read-only public API clarification.
- The clarification subagent itself does not have `clarify_public_api`; it cannot recursively delegate clarification.

## Behavior

- The agent supplies a package path, an identifier, and a specific question.
- The package path may be a sandbox-relative package directory or a Go import path.
- The target package may be in the sandbox, in a dependency module, or in the Go standard library.
- The target identifier names the public API symbol being clarified, such as a function, type, variable, constant, method, or pointer-receiver method.
- The caller should read existing public API documentation before asking for clarification.
- The tool starts a read-only subagent scoped to the resolved target package.
- The subagent answers using the target package context and read-only tools such as `read_file`, `ls`, and `get_public_api`.
- The subagent should distinguish documented behavior, implementation facts, and unknowns instead of guessing beyond available evidence.
- The subagent's ordinary final assistant message is hidden from the user transcript; the clarification answer is presented as the completed tool result.
- For successful clarifications of sandbox packages, the tool records a clarify CAS entry when a CAS root can be selected and authorized.

## Inputs

- `path`: Go package directory or import path.
- `identifier`: public API identifier needing clarification.
- `question`: specific question for the clarification subagent.

## Output

The tool returns the clarification answer written by the read-only subagent.

Errors include invalid parameters, unresolved package paths or import paths, denied target-package reads, subagent setup or invocation failures, and clarify CAS recording failures.

## Presentation

Human-facing output uses an append presentation because clarification is delegated work with a meaningful start and finish.

While the subagent is running, output presents:

```text
• Clarifying API SomeIdentifier in some/pkg
  └ What does this option control?
```

On success, output presents:

```text
• Clarified API SomeIdentifier in some/pkg
  └ The option controls ...
```

The answer body is shown as the tool result, not as a descendant subagent final message.

## Permissions

Sandbox-package reads are authorized before the clarification subagent runs.

The clarification subagent is read-only and scoped to the resolved target package. For dependency and standard-library packages, the tool may run the subagent with the dependency or standard-library root as its effective sandbox so ordinary reads remain confined to the target code unit.

Clarify CAS writes are authorized separately from target-package reads.

## CAS

Clarify CAS records preserve useful API questions and answers as product knowledge tied to the target package content.

When a successful clarification targets a package inside the sandbox, Codalotl may append a CAS record containing the origin package, target package, identifier, question, and answer. If the target package changes, the record naturally becomes stale with the old package hash.

Documentation-improvement workflows can consume clarify CAS records later, using repeated or high-value clarification answers to improve public Go documentation when appropriate.
