# PR

## User Summary (do not modify)

In this PR, refactor internal/gocode.

Target package: internal/gocode

Run these refactors in order:
1. refactor("name": "docs-add", "package": "internal/gocode")
2. refactor("name": "docs-fix", "package": "internal/gocode")
3. refactor("name": "dry", "package": "internal/gocode")
4. refactor("name": "test-cleanup", "package": "internal/gocode")
5. refactor("name": "test-ensure-coverage", "package": "internal/gocode")

Additional instructions:
- After each refactor, inspect the diff before continuing.
- If the diff looks good, commit that refactor separately. Include source changes and relevant CAS files in the commit.
- If the diff looks risky or outside scope, avoid risky fix-forward behavior. Revert, skip with a note in this PR file, or make only a minimal low-risk correction.
- These refactors are intended to be safe and low risk. Do not change public API or behavior except for documentation changes.
- After the final refactor is committed, use the codalotl_cli tool to run:
  codalotl cas recertify internal/gocode --namespaces="docs-fix,refactor-dry,refactor-test-cleanup,refactor-test-ensure-coverage"
- Inspect and commit CAS files produced by recertify.

## Plan

### Package internal/gocode

Run low-risk refactor workflows in the requested order, inspecting and committing each acceptable diff separately:
- [DONE] `docs-add` - added missing important Go documentation in `internal/gocode`; inspected diff and verified with `go test ./internal/gocode`.
- [DONE] `docs-fix` - corrected materially inaccurate/overstated Go documentation in `internal/gocode`; inspected diff, committed the docs-fix CAS record, and verified with `go test ./internal/gocode`.
- [DONE] `dry` - extracted shared comment group text handling for snippet construction; inspected diff, committed the refactor-dry CAS record, and verified with `go test ./internal/gocode`.
- [DONE] `test-cleanup` - simplified test temp directory cleanup, converted snippet lookup assertions to a table, and removed redundant coverage; inspected diff, committed the refactor-test-cleanup CAS record, and verified with `go test ./internal/gocode`.
- `test-ensure-coverage`

After the refactors, run `codalotl cas recertify internal/gocode --namespaces="docs-fix,refactor-dry,refactor-test-cleanup,refactor-test-ensure-coverage"`, inspect the resulting CAS changes, and commit them.

No `SPEC.md` changes are planned because the requested work is documentation/test/internal cleanup only and should not alter public API or behavior.

## Review

Not yet run.

## Summary

Pending.

## State

- Branch: `jn/refactor-internal-gocode`
- PR file: `.prs/2026-05-28_1779980876_refactor-internal-gocode.md`
- Target package: `internal/gocode`
- Workspace was clean before adding this plan.
- Completed `docs-add` in commit `c79c070`; it only added/adjusted Go documentation in `internal/gocode` and passed `go test ./internal/gocode`.
- Completed `docs-fix` in commit `f20b27e`; it only corrected Go documentation in `internal/gocode`, added the relevant `docs-fix` CAS record, and passed `go test ./internal/gocode`.
- Completed `dry` in commit `56be7bc`; it extracted duplicated comment extraction logic into `commentGroupText`, added the relevant `refactor-dry` CAS record, and passed `go test ./internal/gocode`.
- Completed `test-cleanup` in commit `307a0d3`; it cleaned existing tests without changing package behavior, added the relevant `refactor-test-cleanup` CAS record, and passed `go test ./internal/gocode`.
