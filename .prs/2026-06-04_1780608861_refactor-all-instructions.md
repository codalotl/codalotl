# PR

## User Summary (do not modify)

When I run `go run . pr refactor --all-packages --refactor=test-cleanup`, I get these instructions in the PR file:

```text
In this PR, run the test-cleanup refactor across all Go packages in the current module.

Target: all Go packages in the current module
Selected refactor flow: test-cleanup

For each package in the current module:
1. refactor("name": "test-cleanup", "package": "<package>")

Additional instructions:
- Inspect each refactor result and diff before moving to the next package.
- Commit accepted changes with source changes and relevant CAS files. Prefer focused commits per package or small package group.
- Skip no-op packages without a commit and add a note in this PR file.
- If a package looks risky or outside scope, do not fix-forward aggressively; revert/skip it and add a note in this PR file explaining why.
- Due to CAS, packages already up to date for this refactor may be no-ops.
- After final accepted changes, use the codalotl_cli tool for each accepted package that needs recertification:
  codalotl cas recertify <package> --namespaces="refactor-test-cleanup"
- Inspect and commit CAS files produced by recertify.
```

Change this to say to run that refactor across all *needed* Go packages (current module is also incorrect - there could be multiple modules).

Further, explain how to get a list of needed packages: Use `codalotl_cli` to run `codalotl cas ls-packages`. This gives you a list of packages that need to be fixed. (cas ls-packages might need to be added to codalotl_cli).

When the refactor is docs-add specifically, use `codalotl docs status` to find packages that need docs.

The above is the direction. Put on your PM hat and take the above direction across the finish line in terms of specification and design.

## Plan

### [DONE] Package `internal/cli` design/specification

- Update `internal/cli/SPEC.md` so `pr refactor --all-packages` targets packages needing the selected refactor across discovered repo modules, not all packages in one module.
- Specify that generated all-packages PR instructions tell orchestrators to use `codalotl_cli` to discover the needed package list.
- Expose `codalotl cas ls-packages` through the `codalotl_cli` tool so orchestrators can query CAS-backed refactor status.

### [DONE] Package `internal/cli` implementation

- Update `pr refactor --all-packages` help/template text to say "needed packages" and avoid "current module".
- For all refactors except `docs-add`, instruct orchestrators to run `codalotl_cli` with `codalotl cas ls-packages <namespace> --state=outdated` and then refactor only listed packages.
- For `docs-add`, instruct orchestrators to run `codalotl_cli` with `codalotl docs status` and use rows whose `docs_add` status is `needed`.
- Keep existing inspect/commit/skip/recertify guidance, adapted to the discovered package list.
- Add/update tests for generated PR instructions and `codalotl_cli` whitelist exposure.

### Validation

- [DONE] Run focused `internal/cli` tests: `go test ./internal/cli`.
- [DONE] Run full tests: `go test ./...`.
- [DONE] Run SPEC API diff: `go run . spec diff internal/cli`.
- Run review and changed-package SPEC conformance.

## Review

- Formal review against `origin/main` reported one P2 finding: all-packages `docs-fix` discovery via `cas ls-packages docs-fix --state=outdated` can miss packages that have identifier-scoped `docs-fix` CAS records but still need a whole-package docs-fix pass. This is actionable.
- `check_spec_conformance({"only_changed":true})`: `internal/cli` conforms. CAS conformance record produced.

## Summary

Pending.

## State

- Branch: `jn/refactor-all-instructions`.
- Primary package: `internal/cli`.
- Relevant files: `internal/cli/pr_new.go`, `internal/cli/pr_new_test.go`, `internal/cli/commands.go`, `internal/cli/codalotl_cli_tool_test.go`, `internal/cli/SPEC.md`.
- `internal/cli` already had root CLI support for `codalotl cas ls-packages`; implementation now whitelists it in `newCodalotlCLICommandTree`.
- Verification passed before review: `go test ./internal/cli`, `go test ./...`, `go run . spec diff internal/cli`.
