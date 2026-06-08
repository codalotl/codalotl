# `update_usage`

`update_usage` lets a package-mode agent update downstream Go packages that use the selected package.

It is the cross-package editing tool for keeping package users aligned when the selected package's API or expected usage changes.

## Availability

- Available in package-mode agents.
- Available to delegated package agents when their toolset includes downstream usage updates.
- Not available to read-only helper agents.
- Not a generic repository editing tool; it is scoped to packages that use the selected package.

## Behavior

- The agent supplies update instructions and one or more downstream package targets.
- Each target may be a sandbox-relative package directory or a Go import path.
- The tool resolves each target to a Go package in the sandbox and current module.
- Each target package must be a downstream package that imports the selected package.
- Duplicate targets that resolve to the same package are updated once.
- The tool delegates edits to package agents for packages using the selected package/API.
- A separate package-mode agent is spawned for each downstream package being updated.
- Each delegated agent is scoped to its target package and receives the caller's instructions as its task.
- Delegated package agents use package-mode tools and package post-checks where available, so edits remain centered on the downstream package being changed.
- The delegated agents' ordinary final assistant messages are hidden from the user transcript; their reports are presented as the completed `update_usage` result.

## Inputs

- `instructions`: instructions for the delegated package agents describing how downstream usage should change.
- `paths`: array of downstream package directories or Go import paths to update.

## Output

The tool returns a per-package report from the delegated package agents.

When no downstream packages import the selected package, the tool reports that there are no downstream packages to update. When a delegated package agent completes without reporting changes, the package is reported as having no changes reported.

Errors include invalid parameters, empty paths, unresolved package targets, targets outside the sandbox or current module, targets that do not import the selected package, denied write authorization, package-loading or usage-discovery failures, and delegated agent failures.

## Presentation

Human-facing output uses an append presentation because usage updates launch delegated package agents and can take meaningful time.

While delegated agents are running, output presents:

```text
• Updating Usage in pkg/a, pkg/b, pkg/c (N more)
  └ Update callers to use NewClient.
```

On success, output presents:

```text
• Updated Usage in pkg/a, pkg/b, pkg/c (N more)
  └ pkg/a:
    updated client construction

    pkg/b:
    no changes reported
```

The body shows the instructions while work is in progress and the delegated result text after completion.

## Permissions

The selected package is authorized for read before usage discovery runs.

Each downstream target package is authorized for write before its delegated package agent runs. The delegated agent is scoped to that downstream package's code unit, so ordinary reads and edits stay centered on the package using the selected package.
