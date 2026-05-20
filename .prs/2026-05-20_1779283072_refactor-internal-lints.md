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
- [DONE] Run `docs-add`; inspected documentation-only diff and committed it separately.
- [DONE] Run `docs-fix`; inspected documentation-only diff and committed it separately with CAS.
- [DONE] Run `dry`; inspected behavior-preserving helper/constant extraction and committed it separately with CAS.
- [DONE] Run `test-cleanup`; inspected table/helper cleanup and committed it separately with CAS.
- [DONE] Run `test-ensure-coverage`; inspected added coverage and committed it separately with CAS.
- [DONE] Run `codalotl cas recertify internal/lints --namespaces="docs-fix,refactor-dry,refactor-test-cleanup,refactor-test-ensure-coverage"` via `codalotl_cli`; inspected and committed produced CAS files.

### Validation
- [DONE] `go test ./internal/lints`
- [DONE] `go test ./...`
- [DONE] Run review and changed-package SPEC conformance once implementation is complete.

## Review

- `review` against `main`: no findings; verdict `patch is correct`.
- `check_spec_conformance({"only_changed": true})`: initially reported a non-latent minor mismatch between `Step` public API comments in `internal/lints/SPEC.md` and implementation docs. Fixed by syncing the SPEC Public API comments, reviewed the SPEC edit, and reran conformance for `internal/lints`; it now conforms.

## Summary

Refactored `internal/lints` with the requested safe internal passes:

- Added and fixed documentation for lint configuration APIs.
- DRYed repeated lint step constants, command construction, width normalization, runner setup, and special-step checks without changing public API or runtime behavior.
- Cleaned up tests with table-driven cases and shared helpers.
- Added coverage for default step resolution, validation errors, reflow in-process execution, and `Run` input validation.
- Recertified requested CAS namespaces and recorded changed-package SPEC conformance.

Validation:
- `go test ./internal/lints`
- `go test ./...`
- Review verdict: patch correct, no findings.
- SPEC conformance: `internal/lints` conforms after syncing SPEC public API comments.

## State

- Branch: `jn/refactor-internal-lints`.
- Active PR file: `.prs/2026-05-20_1779283072_refactor-internal-lints.md`.
- Target package: `internal/lints`.
- User specifically requested the canned refactors in order, each inspected and committed separately when safe, followed by CAS recertify.
- `docs-add` added internal helper documentation in `internal/lints/lints.go`; `go test ./internal/lints` passed.
- `docs-fix` clarified `Step.Situations` docs and added `.codalotl/cas/docs-fix-1/6b/f6e62d2e0c7d2eb00bd834f241ee126a2643974430de620884c80f3212a201`; `go test ./internal/lints` passed.
- `dry` extracted constants/helpers in `internal/lints/lints.go` and added `.codalotl/cas/refactor-dry-1/0e/b165a2964b7ef4f18661136852283fc954e20069e21784706cb6edef41901b`; `go test ./internal/lints` passed.
- `test-cleanup` refactored tests in `internal/lints/lints_test.go` and added `.codalotl/cas/refactor-test-cleanup-1/4f/1bb9307f88513565c342973056492805e973c38624adff4653f3b3aa8f2023`; `go test ./internal/lints` passed.
- `test-ensure-coverage` added focused coverage in `internal/lints/lints_test.go` and added `.codalotl/cas/refactor-test-ensure-coverage-1/3f/d613e23db4accaeec704ca1e645ac5de44eba16326fb009d149650293b08ca`; `go test ./internal/lints` passed.
- Recertify produced and committed new docs-fix/refactor-dry/refactor-test-cleanup CAS records.
- Final validation: `go test ./...` passed; review had no findings; SPEC conformance passed for `internal/lints` after syncing SPEC public API comments.
