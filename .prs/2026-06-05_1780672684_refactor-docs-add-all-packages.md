# PR

## User Summary (do not modify)

In this PR, run the docs-add refactor across needed Go packages from discovered repo modules.

Target: needed Go packages discovered for docs-add
Selected refactor flow: docs-add

Discover needed packages first:
- Use the codalotl_cli tool to run:
  codalotl docs status
- Use packages whose docs_add status is needed as the discovered needed package list.

Discovery commands may return packages from multiple Go modules.

For each discovered needed package, run refactor("name": "docs-add", "package": "<package>").

Additional instructions:
- Refactor only packages in the discovered needed package list.
- If discovery finds no needed packages, note that in this PR file and stop.
- Inspect each refactor result and diff before moving to the next package.
- Commit accepted changes with source changes and relevant CAS files. Prefer focused commits per package.
- Skip no-op packages without a commit and add a note in this PR file.
- If a package looks risky or outside scope, do not fix-forward aggressively; revert/skip it and add a note in this PR file explaining why.
- No CAS namespace is currently recertifiable specifically for this refactor. If accepted package changes invalidate other applicable CAS records, recertify those after final changes from the module containing each package or with module-local package arguments.

## Plan

### Discover docs-add targets [DONE]

`codalotl docs status` reported these packages with `docs_add` needed:
- `./internal/agentformatter`
- `./internal/applypatch`
- `./internal/cli`
- `./internal/docubot`
- `./internal/llmstream`
- `./internal/q/termformat`
- `./internal/tui`

### Run docs-add per target package

Run `refactor("docs-add")` on each discovered package only. Inspect each result and diff before moving to the next target. Commit accepted package changes in focused commits.

### Validate and complete

Run final review and changed-package SPEC conformance. Address required conformance failures or actionable review feedback, then write PR summary.

## Review

Not run yet.

## Summary

Pending.

## State

- Branch: `jn/refactor-docs-add-all-packages`
- Active PR file: `.prs/2026-06-05_1780672684_refactor-docs-add-all-packages.md`
- Discovery source: `codalotl docs status`

