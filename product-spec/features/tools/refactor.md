# `refactor`

`refactor` runs a named refactor workflow against a Go package.

It is a high-level workflow tool for recurring package maintenance work.

## Inputs

- `name`: refactor name.
- `package`: Go package directory, current-module import path, or current-module relative package path.

## Output

On success, the tool returns a structured result with:

- `name`: refactor name that ran.
- `package`: resolved package directory relative to the module root.
- `status`: one of `applied`, `no_opportunity`, or `already_applied`.
- `message`: human-readable status message.
- `edited-files`: package-relative files changed by the refactor.
- `saved-cas-record`: refactor-owned CAS record path, when the refactor wrote one.

Errors include invalid parameters, unknown refactor names, package resolution failures, authorization failures, delegated CLI failures, subagent failures, and CAS read/write failures.

## Behavior

- The agent supplies a refactor name and a target Go package.
- The package may be supplied as an absolute package directory, a current-module-relative package directory, or a current-module import path.
- Package resolution must stay inside the current Go module and inside the sandbox.
- Standard-library packages, module dependencies, and packages outside the sandbox are rejected.
- Unknown refactor names are usage errors.
- Refactors are package-local unless their documented workflow uses external workflow state such as CAS records.
- The result always includes the resolved package, a status, and the edited file list.
- Edited files are package-relative and include added, removed, and modified files. Moved files may appear as a removal and an addition.

## Refactors

Available refactors:

- `docs-add`: adds missing important Go documentation by delegating to `codalotl docs add --important <package>`.
- `docs-fix`: fixes materially false Go documentation by delegating to `codalotl docs fix <package>`.
- `docs-improve-from-clarify`: uses in-play `clarify_public_api` Q/A CAS records to improve public Go documentation when the clarification naturally belongs in the target package's docs.
- `dry`: asks a limited package-mode subagent to share helpers and combine similar helper logic while preserving behavior and public API.
- `test-cleanup`: asks a limited package-mode subagent to clean up existing tests without adding missing coverage.
- `test-ensure-coverage`: asks a limited package-mode subagent to add worthwhile test coverage for public APIs and important edge cases.

## Status

The result status distinguishes:

- `applied`: the refactor completed and made edits.
- `no_opportunity`: the refactor ran successfully but found no worthwhile edits.
- `already_applied`: a CAS-backed refactor was skipped because the current package code unit already has a matching refactor CAS record.
- error: the refactor could not run or did not complete successfully.

Human-facing messages use ordinary phrasing such as `Successfully applied refactor`, `No refactoring opportunities found`, and `Refactor already applied`.

## CAS

- `docs-add`, `docs-fix`, and `docs-improve-from-clarify` do not write refactor-owned CAS records.
- `dry`, `test-cleanup`, and `test-ensure-coverage` check and write refactor-owned CAS records for the package code unit.
- A CAS hit returns `already_applied` without running the refactor again.
- After a successful CAS-backed run, the tool writes a refactor-owned CAS record even when no edits were needed.
- If a refactor fails, it does not write a refactor-owned CAS record.
- If writing the CAS record fails after edits were made, the tool reports an error and leaves the filesystem edits in place.

## Presentation

Example display while running:

```text
• Refactoring test-cleanup in internal/foo
```

Example display after completion:

```text
• Refactored test-cleanup in internal/foo
  └ Successfully applied refactor
```

When no change is needed:

```text
• Refactored dry in internal/foo
  └ No refactoring opportunities found
```

When skipped by CAS:

```text
• Refactored test-ensure-coverage in internal/foo
  └ Refactor already applied
```

## Permissions

Package reads and writes are authorized before the workflow runs.

The resolved package must be in the current module and inside the sandbox. Prompt-style refactors use package-mode authorization for the selected package code unit. CAS-backed refactors also require authorized access to the CAS database root, which may be outside the sandbox according to the product CAS rules.
