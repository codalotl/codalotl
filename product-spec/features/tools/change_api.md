# `change_api`

`change_api` requests a public API or public behavior change in an imported Go package.

It is the package-mode tool for changing an imported package without having the current package agent directly edit across package boundaries.

## Inputs

- `path`: Go package directory relative to the sandbox, or a Go import path. It must resolve to a directly imported upstream package inside the sandbox.
- `instructions`: what to change and why. The instructions should include enough context for a new package agent to update the target package safely.

## Output

The tool returns the final result written by the delegated package agent after it attempts the requested API or behavior change.

Errors include invalid parameters, unresolved package paths or import paths, targets outside the sandbox, standard-library or dependency-cache targets, targets that are not direct imports of the current package, denied reads or writes, package-loading failures, unavailable subagent execution, and delegated-agent failures.

## Behavior

- The agent supplies a target package and instructions for the requested change.
- The target package may be a sandbox-relative package directory or a Go import path.
- The target must resolve to a package inside the sandbox.
- The target must be directly imported by the current package.
- The target must not be the current package itself.
- Standard-library packages and dependency packages in the module cache cannot be changed.
- The requested change may alter exported API shape or public behavior.
- The tool delegates the edit to a package agent scoped to the imported target package, instead of giving the current package agent direct cross-package editing access.
- The delegated package agent works with its own package-mode context and package-scoped editing tools.
- The delegated agent's final result is returned as the `change_api` tool result rather than appearing as a separate chat message.

## Presentation

Example display while running:

```text
• Changing API in some/pkg
  └ Add a method needed by the current package.
```

Example display after completion:

```text
• Changed API in some/pkg
  └ Updated the target package and verified its package tests.
```

## Permissions

The current package is authorized for read access before imports are checked.

The target package is authorized for write access before delegated editing begins.

The delegated package agent is scoped to the resolved target package code unit. This preserves package-mode boundaries while still letting the overall task coordinate a necessary upstream API or behavior change.
