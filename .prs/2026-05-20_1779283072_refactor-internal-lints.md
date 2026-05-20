# PR

## User Summary (do not modify)

In this PR, refactor internal/lints.

Target package: internal/lints

Run these refactors in order:
1. refactor("name": "docs-add", "package": "internal/lints")
2. refactor("name": "docs-fix", "package": "internal/lints")
3. refactor("name": "dry", "package": "internal/lints")
4. refactor("name": "test-cleanup", "package": "internal/lints")
5. refactor("name": "test-ensure-coverage", "package": "internal/lints")

Additional instructions:
- After each refactor, inspect the diff before continuing.
- If the diff looks good, commit that refactor separately. Include source changes and relevant CAS files in the commit.
- If the diff looks risky or outside scope, avoid risky fix-forward behavior. Revert, skip with a note in this PR file, or make only a minimal low-risk correction.
- These refactors are intended to be safe and low risk. Do not change public API or behavior except for documentation changes.
- After the final refactor is committed, use the codalotl_cli tool to run:
  codalotl cas recertify internal/lints --namespaces="docs-fix,refactor-dry,refactor-test-cleanup,refactor-test-ensure-coverage"
- Inspect and commit CAS files produced by recertify.

## Plan

### Package internal/lints
- Run `docs-add`; inspect the diff and commit it separately if safe.
- Run `docs-fix`; inspect the diff and commit it separately if safe.
- Run `dry`; inspect the diff and commit it separately if safe.
- Run `test-cleanup`; inspect the diff and commit it separately if safe.
- Run `test-ensure-coverage`; inspect the diff and commit it separately if safe.
- Run `codalotl cas recertify internal/lints --namespaces="docs-fix,refactor-dry,refactor-test-cleanup,refactor-test-ensure-coverage"` via `codalotl_cli`; inspect and commit produced CAS files.

### Validation
- After the refactors, run package tests or broader tests as appropriate.
- Run review and changed-package SPEC conformance once implementation is complete.

## Review

Not yet run.

## Summary

TBD.

## State

- Branch: `jn/refactor-internal-lints`.
- Active PR file: `.prs/2026-05-20_1779283072_refactor-internal-lints.md`.
- Target package: `internal/lints`.
- User specifically requested the canned refactors in order, each inspected and committed separately when safe, followed by CAS recertify.
