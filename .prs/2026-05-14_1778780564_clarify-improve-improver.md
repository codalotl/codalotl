# PR

## User Summary (do not modify)

Background: clarify_public_api is used when a package tries to use another package, but needs clarification on how to use the api. My hypothesis is that this represents an opportunity to improve the documentation. So clarify_public_api records the q/a using CAS.

Then, `codalotl docs improve-from-clarify` can be used to consume these files and improve the docs.

Unfortunately, this just doesn't work well. Usually the answers are just kinda written in the docs of the identifier, and it feels really out of place. An amazing human engineer would NEVER document a function like this.

I think one problem is that we use the docubot system to improve docs, but docubot has limited context and agency. To improve symbol X, docubot sends the LLM X and X's in/out nodes. It then possibly only has the liberty of documenting X, or not.

However, I suspect a lot of this documentation is best done in doc.go, or a related type. Basically, the original caller of clarify_public_api didn't have the right mental model.

So, let's do the following:

- Remove `codalotl docs improve-from-clarify`
- add a refactor subcommand: `docs-improve-from-clarify`. Update orchestrator prompt to use this.
- This refactor subcommand doesn't call into docubot. Instead, it just spins up a normal package-mode agent with a custom prompt and the q/as.

## Plan

### Phase 1: Add clarify-doc improvement refactor

#### Package `internal/tools/refactor` [DONE]
- Add `docs-improve-from-clarify` as a package-local refactor.
- It should use in-play `clarify_public_api` CAS Q/A records for the target package, invoke `package_mode_default_context` with a custom prompt and those Q/As, detect edited package files, and report normal refactor status.
- It should let the package-mode agent improve docs wherever the confusion is best resolved, not necessarily on the originally questioned identifier.
- It should delete consumed clarify records after successful agent completion, including no-op runs. Preserve records on failure.
- It should not use refactor-owned CAS; clarify CAS records are the workflow state.
- `internal/tools/refactor/SPEC.md` has been updated.
	- Implemented in `28de7f1 add clarify docs refactor`.

#### Package `internal/gocas/casclarify` [DONE]
- Prefer existing `FindInPlay` and `InPlayRecord.Delete` support.
- Add narrowly scoped helpers only if `internal/tools/refactor` needs safer target filtering or record consumption semantics.
	- No package changes were needed; existing public API was sufficient.

### Phase 2: Remove the old CLI/docubot path and update orchestrator guidance

#### Package `internal/cli`
- Remove `codalotl docs improve-from-clarify` command registration, command implementation/tests, and `codalotl_cli` catalog exposure.
- `internal/cli/SPEC.md` has been updated.

#### Package `internal/docubot`
- Remove clarification-improvement API/behavior/tests from docubot.
- Keep normal docubot documentation add/improve/fix behavior unchanged.
- `internal/docubot/SPEC.md` has been updated.

#### Package `internal/agentbuilder`
- Update the PR orchestrator prompt data to use the `refactor` tool's `docs-improve-from-clarify` workflow for clarify-public-api CAS records.
- `internal/agentbuilder/SPEC.md` has been updated.

#### Product docs and tests
- Update product docs and tests that mention `codalotl docs improve-from-clarify` or assert the `codalotl_cli` command catalog.
- Expect CLI tests and possibly noninteractive integration replays to need adjustment because the exposed tool surface changes.

## Review

No implementation review yet. SPEC review completed for `internal/tools/refactor`, `internal/agentbuilder`, `internal/cli`, and `internal/docubot`; suggested wording refinements were applied.

## Summary

TBD.

## State

- Branch: `jn/clarify-improve-improver`.
- PR file: `.prs/2026-05-14_1778780564_clarify-improve-improver.md`.
- Core issue: old clarify doc improver uses `docubot.ImproveFromClarifications`, which tends to paste answers into symbol docs. Desired workflow uses a normal package-mode agent so docs can move to `doc.go`, related types, or another natural location.
- Relevant existing code:
  - Old CLI command: `internal/cli/docs_improve_from_clarify.go`.
  - Old docubot API: `internal/docubot/improve_from_clarifications.go`.
  - Refactor tool registry/implementation: `internal/tools/refactor/refactor.go`.
  - Clarify CAS: `internal/gocas/casclarify`.
  - Orchestrator prompt data: `internal/agentbuilder/data/pr-orchestrator.prompt.md`.

