# `refactor`

`refactor` lets an agent run a named, package-local refactor workflow against a Go package.

It is a high-level workflow tool. Some refactors delegate to Codalotl CLI commands, while others launch focused package-mode subagents with canned prompts.

## Availability

- Available to agents that are allowed to run package-local refactor workflows.
- Available to orchestrator-style agents for recurring package maintenance work.
- Available in generic contexts when the tool can resolve the requested package inside the current module and sandbox.

## Behavior

- The agent supplies a refactor name and a target Go package.
- The package may be supplied as an absolute package directory, a current-module-relative package directory, or a current-module import path.
- Package resolution must stay inside the current Go module and inside the sandbox. Standard-library packages, module dependencies, and packages outside the sandbox are rejected.
- Unknown refactor names are usage errors.
- The tool description lists the available refactor names and brief descriptions so the agent can choose a supported workflow.
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

## Delegation

`docs-add` and `docs-fix` delegate to the Codalotl CLI through the whitelisted Codalotl command tool. Their command output may stream visibly while the refactor is running.

Prompt-style refactors launch a package-mode subagent:

- `docs-improve-from-clarify` uses a package-mode agent with default package context.
- `dry`, `test-cleanup`, and `test-ensure-coverage` use a limited package-mode agent.

Prompt-style subagents receive package-scoped context and tools. Their descendant events remain visible under the refactor, so users can see the package work that the high-level tool caused.

## CAS

Each refactor has a CAS policy:

- `cas-ignore`: the refactor does not write refactor-owned CAS records. It may delegate to another command that writes its own CAS records, or it may consume external workflow CAS records.
- `cas-code-unit`: the refactor checks and writes a refactor-owned CAS record for the package code unit.

`docs-add`, `docs-fix`, and `docs-improve-from-clarify` use `cas-ignore`.

`dry`, `test-cleanup`, and `test-ensure-coverage` use `cas-code-unit`. Before running, they check for an up-to-date CAS record. A hit returns `already_applied` without launching the subagent. After a successful run, they write a refactor-owned CAS record even when no edits were needed.

Refactor-owned CAS namespaces are named like `refactor-dry`, with versioned filesystem namespaces like `refactor-dry-1`. Records use the code-unit hash mode and store metadata indicating the refactor was applied and which package-relative files were edited.

If a refactor fails, it does not write a refactor-owned CAS record. If writing the CAS record fails after edits were made, the tool reports an error and leaves the filesystem edits in place.

`docs-fix` delegates to a command that writes the docs-fix CAS record; the `refactor` result reports edited files but does not report delegated CAS paths. `docs-improve-from-clarify` consumes matching in-play clarify records after a successful run, including successful no-op runs, and preserves them on failure.

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

## Presentation

Human-facing output uses append presentation because refactors may run for a while and may contain nested subagent or CLI activity.

In progress:

```text
• Refactoring test-cleanup in internal/foo
```

On completion:

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
