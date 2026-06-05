# PR

## User Summary (do not modify)

In this PR, refactor internal/cli.

Target package: internal/cli
Selected refactor flow: all refactors for one package

Run these refactors in order:
1. refactor("name": "docs-add", "package": "internal/cli")
2. refactor("name": "docs-fix", "package": "internal/cli")
3. refactor("name": "dry", "package": "internal/cli")
4. refactor("name": "test-cleanup", "package": "internal/cli")
5. refactor("name": "test-ensure-coverage", "package": "internal/cli")

Additional instructions:
- After each refactor, inspect the diff before continuing.
- If the diff looks good, commit that refactor separately. Include source changes and relevant CAS files in the commit.
- If a refactor result is a no-op, skip it with a note in this PR file.
- If the diff looks risky or outside scope, avoid risky fix-forward behavior. Revert, skip with a note in this PR file, or make only a minimal low-risk correction.
- These refactors are intended to be safe and low risk. Do not change public API or behavior except for documentation changes.
- After the final refactor is committed, use the codalotl_cli tool to run:
  codalotl cas recertify internal/cli --namespaces="docs-fix,refactor-dry,refactor-test-cleanup,refactor-test-ensure-coverage"
- Inspect and commit CAS files produced by recertify.

## Plan

### Package internal/cli

Run the requested safe refactor sequence one refactor at a time. After each refactor:
- Inspect the diff.
- Commit useful, in-scope changes separately, including relevant CAS artifacts.
- If the refactor is a no-op, record the skipped result here.
- If the diff is risky or out of scope, revert or avoid it and record the decision here.

#### Refactor sequence

0. [DONE] Run `docs-improve-from-clarify` for `internal/cli` because `.codalotl/cas/clarify-public-api-1` is present. Result: no opportunities found; no source/CAS diff to commit.
1. [DONE] `docs-add` for `internal/cli`. Result: refactor reported applied, but produced no workspace diff.
2. [DONE] `docs-fix` for `internal/cli`. Result: committed documentation-only corrections with CAS artifact.
3. [DONE] `dry` for `internal/cli`. Result: committed helper extraction/deduplication with CAS artifact; `go test ./internal/cli` passed.
4. `test-cleanup` for `internal/cli`.
5. `test-ensure-coverage` for `internal/cli`.
6. Run `codalotl cas recertify internal/cli --namespaces="docs-fix,refactor-dry,refactor-test-cleanup,refactor-test-ensure-coverage"`.
7. Inspect and commit recertification CAS artifacts.

## Review

Not started.

## Summary

Not written yet.

## State

- Branch: `jn/refactor-internal-cli`.
- PR file: `.prs/2026-06-05_1780690605_refactor-internal-cli.md`.
- Target package: `internal/cli`.
- Workspace was clean before planning.
- `.codalotl/cas/clarify-public-api-1` exists; run `docs-improve-from-clarify` before the requested refactor sequence.
- `docs-improve-from-clarify` on `internal/cli` returned `no_opportunity`.
- `docs-add` on `internal/cli` reported applied but left the workspace clean.
- `docs-fix` adjusted comments in `cli.go`, `config.go`, `iterate_command.go`, `monitoring.go`, and `pr_new.go`; committed as `a27b9dd`.
- `dry` introduced `writeAlignedTable`, shared CAS package discovery / DB helpers, and reused package loading; committed as `8c39bc9`.
