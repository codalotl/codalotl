# `update_usage`

`update_usage` spawns new agent(s) that updates downstream Go packages that use the selected package (in package mode). It allows a package-mode-like agent to change the public API of the selected package, then update callsites to conform to it.

Each spawned agent has a new context, so clear instructions need to be provided. Each agent will operate in a package-mode-like jail against the package it is intended to update.

## Inputs

- `instructions`: instructions for the delegated package agents describing how downstream usage should change.
- `paths`: array of downstream package directories or Go import paths to update.

## Output

The tool returns a per-package report from the delegated package agents.

When no downstream packages import the selected package, the tool reports that there are no downstream packages to update. When a delegated package agent completes without reporting changes, the package is reported as having no changes reported.

Errors include invalid parameters, empty paths, unresolved package targets, targets outside the sandbox or current module, targets that do not import the selected package, denied write authorization, package-loading or usage-discovery failures, and delegated agent failures.

Example output:

```text
example.com/clarifyintegration:
I checked the assigned package at `.` and there are no `.HasTag(...)` call sites, so I made no code changes.

example.com/clarifyintegration/inventory:
I checked `inventory` and there are no `.HasTag(...)` call sites in this package, so no code changes were required.

example.com/clarifyintegration/pricing:
Updated `pricing/quote.go` to use `product.MatchesTag(...)` instead of the renamed `product.HasTag(...)`.

Result:
- Build issue is resolved.
- Package tests pass: `go test ./pricing`
```

## Behavior

- The agent supplies update instructions and one or more downstream package targets.
- Each target may be a sandbox-relative package directory or a Go import path.
- The tool resolves each target to a Go package in the sandbox and current module and are de-duplicated.
- Each target package must be a downstream package that imports the selected package.
    - This tool cannot be used to create new dependencies to the originating package.
- The tool spawns agent(s) in ~package-mode (one per target package):
    - The agent is locked to its package and has typical tools like `apply_patch`, `run_tests`, etc.
    - It has access to read-only cross-package tools like `get_public_api` and `clarify_public_api`.
    - But it cannot spawn mutative agents like `change_api` or recursively call `update_usage`.
- Delegated agent reports are aggregated and returned as the completed `update_usage` result.

## Presentation

Example display (NOTE: text/instructions are simplified, and will be more detailed in real-world uses):

```text
• Updating Usage in pkg/a, pkg/b, pkg/c (N more)
  └ Update callers to use NewClient.
```

Example display after completion:

```text
• Updated Usage in pkg/a, pkg/b, pkg/c (N more)
  └ pkg/a:
    updated client construction

    pkg/b:
    no changes reported
```

The spawned agents emit events, which are indented and displayed between the `Updating` and `Updated` lines.
