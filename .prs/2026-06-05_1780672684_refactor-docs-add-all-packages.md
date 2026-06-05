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

### Run docs-add per target package [DONE]

Run `refactor("docs-add")` on each discovered package only. Inspect each result and diff before moving to the next target. Commit accepted package changes in focused commits.

Completed:
- `./internal/agentformatter`
- `./internal/applypatch`
- `./internal/cli`
- `./internal/docubot`
- `./internal/llmstream`
- `./internal/q/termformat`
- `./internal/tui`

Notes:
- `./internal/applypatch` initially needed manual documentation cleanup after docs-add snippet application failed, then a final docs-add pass completed successfully.

### Validate and complete [DONE]

Run final review and changed-package SPEC conformance. Address required conformance failures or actionable review feedback, then write PR summary.

## Review

Initial review:
- Code review: no findings; patch judged correct.
- SPEC conformance: one non-latent trivial issue in `internal/agentformatter`; `MinTerminalWidth` comment implied wrapping at the threshold, while behavior and SPEC wrap only above the threshold. Fixed by updating the comment only.

Latest review:
- Code review: no findings; patch judged correct.
- SPEC conformance: changed packages conform. The rerun checked the updated `internal/agentformatter` state after prior conforming packages wrote CAS records.
- Additional validation: `go test ./...` passed; final `codalotl docs status` reports `docs_add` current for all discovered target packages.

## Summary

Adds missing documentation comments for all packages discovered with `docs_add` needed:
- `internal/agentformatter`
- `internal/applypatch`
- `internal/cli`
- `internal/docubot`
- `internal/llmstream`
- `internal/q/termformat`
- `internal/tui`

Also records SPEC conformance CAS entries for conforming changed packages.

Validation:
- `codalotl docs status` reports `docs_add` current for all discovered target packages.
- Code review reported no findings and judged the patch correct.
- Changed-package SPEC conformance passed after a documentation-only threshold wording fix in `internal/agentformatter`.
- `go test ./...` passed.

## State

- Branch: `jn/refactor-docs-add-all-packages`
- Active PR file: `.prs/2026-06-05_1780672684_refactor-docs-add-all-packages.md`
- Discovery source: `codalotl docs status`
- Implementation status: docs-add targets are complete; `codalotl docs status` reports `docs_add` current for all discovered target packages.
- Conformance follow-up: `internal/agentformatter` threshold wording fixed without behavior changes.
- Validation status: code review clean, SPEC conformance clean, full Go test suite passing.

