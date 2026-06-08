# `change_api`

`change_api` lets a package-mode agent request a change to the public API or public behavior of an upstream Go package that the selected package directly imports.

It is the package-mode tool for changing an imported package without having the current package agent directly edit across package boundaries.

## Availability

- Available in package-mode agents.
- Available to delegated package agents when they need to coordinate changes to a directly imported upstream package.
- Not available as a generic filesystem editing tool.

## Behavior

- The agent supplies a target package and instructions for the requested change.
- The target package may be a sandbox-relative package directory or a Go import path.
- The target must resolve to a package inside the sandbox.
- The target must be directly imported by the current package.
- The target must not be the current package itself.
- Standard-library packages and dependency packages in the module cache cannot be changed.
- The requested change may alter exported API shape, such as adding methods, changing function parameters, or updating exported struct fields.
- The requested change may alter public behavior without changing signatures, such as fixing a bug observed by the current package.
- The tool delegates the edit to a package agent scoped to the imported target package, rather than giving the current package agent direct cross-package editing access.
- The delegated package agent receives the instructions as its user task and works with its own package-mode context and package-scoped editing tools.
- The delegated package agent may run package checks and lint fixes that normally apply to package-mode edits.
- The delegated package agent's ordinary final assistant message is hidden from the user transcript; its final result is presented as the completed tool result.

## Inputs

- `path`: Go package directory relative to the sandbox, or a Go import path. It must resolve to a directly imported upstream package inside the sandbox.
- `instructions`: what to change and why. The instructions should include enough context for a new package agent to update the target package safely.

## Output

The tool returns the final result written by the delegated package agent after it attempts the requested API or behavior change.

Errors include invalid parameters, unresolved package paths or import paths, targets outside the sandbox, standard-library or dependency-cache targets, targets that are not direct imports of the current package, denied reads or writes, package-loading failures, unavailable subagent execution, and delegated-agent failures.

## Presentation

Human-facing output uses an append presentation because `change_api` starts delegated package work with a meaningful start and finish.

While the delegated package agent is running, output presents:

```text
• Changing API in some/pkg
  └ Add a method needed by the current package.
```

On success, output presents:

```text
• Changed API in some/pkg
  └ Updated the target package and verified its package tests.
```

The result body is shown as the tool result, not as a descendant subagent final message.

## Permissions

The current package is authorized for read access before imports are checked.

The target package is authorized for write access before delegated editing begins.

The delegated package agent is scoped to the resolved target package code unit. This preserves package-mode boundaries while still letting the overall task coordinate a necessary upstream API or behavior change.
