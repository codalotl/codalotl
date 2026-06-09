# `implement`

`implement` delegates implementation work for one Go package to a package-mode agent.

It is the orchestrator's package-focused editing tool: the orchestrator decides what package needs work, then hands that package and task instructions to an agent with package context and package-scoped tools.

## Inputs

- `path`: Go package directory relative to the sandbox, or a Go import path.
- `instructions`: what to change and why. The instructions should include enough context for a new package agent to update the package safely.

## Output

The tool returns the final result written by the delegated package agent after it attempts the requested implementation.

The result is plain text. It usually summarizes the implementation outcome, changed behavior, verification performed, and any remaining blocker or risk the package agent reports.

Errors include invalid parameters, unresolved package paths or import paths, targets outside the sandbox, denied reads or writes, package-loading failures, unavailable subagent execution, and delegated-agent failures.

## Behavior

- The orchestrator supplies one target package and implementation instructions.
- The target package may be a sandbox-relative Go package directory or a Go import path.
- The target package must resolve inside the sandbox.
- The tool starts a package-mode agent scoped to the resolved package.
- The delegated package agent receives the instructions as its user task.
- The delegated package agent starts with normal package-mode context and package-scoped tools.
- The delegated package agent may coordinate package-aware upstream or downstream changes when the implementation requires them.
- The delegated agent's final result is returned as the `implement` tool result rather than appearing as a separate chat message.

## Presentation

Example display while running:

```text
• Implementing some/pkg
  └ Add the new behavior and verify package tests.
```

Example display after completion:

```text
• Implemented some/pkg
  └ Updated the package and verified its tests.
```

## Permissions

The target package is resolved and authorized before delegated editing begins.

The delegated package agent is scoped to the resolved target package code unit. This keeps the implementation centered on the selected package while still allowing package-mode coordination tools for cross-package work when the instructions require it.
