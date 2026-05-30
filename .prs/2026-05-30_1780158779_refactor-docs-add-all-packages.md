# PR

## User Summary (do not modify)

In this PR, run the docs-add refactor across all Go packages in the current module.

Target: all Go packages in the current module
Selected refactor flow: docs-add

For each package in the current module:
1. refactor("name": "docs-add", "package": "<package>")

Additional instructions:
- Inspect each refactor result and diff before moving to the next package.
- Commit accepted changes with source changes and relevant CAS files. Prefer focused commits per package or small package group.
- Skip no-op packages without a commit and add a note in this PR file.
- If a package looks risky or outside scope, do not fix-forward aggressively; revert/skip it and add a note in this PR file explaining why.
- Due to CAS, packages already up to date for this refactor may be no-ops.
- No CAS namespace is currently recertifiable specifically for this refactor. If accepted package changes invalidate other applicable CAS records, recertify those after final changes.

## Plan

### Preflight
- [DONE] Identify all Go packages in the current module with `go list ./...`.
- [DONE] Clarify-public-api CAS records are present. Per orchestrator CAS policy, resolve them with `docs-improve-from-clarify` before continuing broad `docs-add` work, then commit any accepted doc improvements with consumed CAS records.
- No `SPEC.md` changes are planned for this PR because the requested work is documentation-only and should not change package behavior.

### No-op/skipped notes
- `docs-improve-from-clarify` returned `no_opportunity` for `internal/gocas`, `internal/q/cas`, and `internal/subscriptions/openaisub`; no files or CAS records changed.

### Refactor batches

Run `refactor("name": "docs-add", "package": "<package>")` for each package below, inspect the result and diff, commit useful changes in focused batches, and note no-op/skipped packages here.

#### Batch 1: root and core agent/app packages
- `github.com/codalotl/codalotl`
- `github.com/codalotl/codalotl/internal/agent`
- `github.com/codalotl/codalotl/internal/agentbuilder`
- `github.com/codalotl/codalotl/internal/agentformatter`
- `github.com/codalotl/codalotl/internal/agentregistry`
- `github.com/codalotl/codalotl/internal/agentsmd`
- `github.com/codalotl/codalotl/internal/applypatch`
- `github.com/codalotl/codalotl/internal/cli`

#### Batch 2: code/doc/git/gocas packages
- `github.com/codalotl/codalotl/internal/codeunit`
- `github.com/codalotl/codalotl/internal/detectlang`
- `github.com/codalotl/codalotl/internal/diff`
- `github.com/codalotl/codalotl/internal/docubot`
- `github.com/codalotl/codalotl/internal/docubot/cmd`
- `github.com/codalotl/codalotl/internal/gittools`
- `github.com/codalotl/codalotl/internal/gittools/cmd/changedfiles`
- `github.com/codalotl/codalotl/internal/gittools/cmd/mergebase`
- `github.com/codalotl/codalotl/internal/gocas`
- `github.com/codalotl/codalotl/internal/gocas/casclarify`
- `github.com/codalotl/codalotl/internal/gocas/casconformance`

#### Batch 3: Go analysis/refactor packages
- `github.com/codalotl/codalotl/internal/goclitools`
- `github.com/codalotl/codalotl/internal/gocode`
- `github.com/codalotl/codalotl/internal/gocodecontext`
- `github.com/codalotl/codalotl/internal/gocodetesting`
- `github.com/codalotl/codalotl/internal/gograph`
- `github.com/codalotl/codalotl/internal/gopackagediff`
- `github.com/codalotl/codalotl/internal/gorenamer`
- `github.com/codalotl/codalotl/internal/gotypes`
- `github.com/codalotl/codalotl/internal/gousage`

#### Batch 4: orchestration/runtime packages
- `github.com/codalotl/codalotl/internal/initialcontext`
- `github.com/codalotl/codalotl/internal/iterate`
- `github.com/codalotl/codalotl/internal/lints`
- `github.com/codalotl/codalotl/internal/llmmodel`
- `github.com/codalotl/codalotl/internal/llmstream`
- `github.com/codalotl/codalotl/internal/llmstream/anthropic`
- `github.com/codalotl/codalotl/internal/llmstream/gemini`
- `github.com/codalotl/codalotl/internal/mockllm/mockopenai`
- `github.com/codalotl/codalotl/internal/noninteractive`
- `github.com/codalotl/codalotl/internal/noninteractive/integration`
- `github.com/codalotl/codalotl/internal/noninteractive/integration/cmd/create`
- `github.com/codalotl/codalotl/internal/prompt`

#### Batch 5: `internal/q` support packages
- `github.com/codalotl/codalotl/internal/q/cas`
- `github.com/codalotl/codalotl/internal/q/cascade`
- `github.com/codalotl/codalotl/internal/q/cli`
- `github.com/codalotl/codalotl/internal/q/clipboard`
- `github.com/codalotl/codalotl/internal/q/cmdrunner`
- `github.com/codalotl/codalotl/internal/q/health`
- `github.com/codalotl/codalotl/internal/q/remotemonitor`
- `github.com/codalotl/codalotl/internal/q/semver`
- `github.com/codalotl/codalotl/internal/q/sseclient`
- `github.com/codalotl/codalotl/internal/q/termformat`
- `github.com/codalotl/codalotl/internal/q/termformat/cmd`
- `github.com/codalotl/codalotl/internal/q/tui`
- `github.com/codalotl/codalotl/internal/q/tui/cmd`
- `github.com/codalotl/codalotl/internal/q/tui/tuicontrols`
- `github.com/codalotl/codalotl/internal/q/tui/tuicontrols/cmd/chatlog`
- `github.com/codalotl/codalotl/internal/q/uni`

#### Batch 6: skills/tools/tui/update packages
- `github.com/codalotl/codalotl/internal/reorgbot`
- `github.com/codalotl/codalotl/internal/simplelogger`
- `github.com/codalotl/codalotl/internal/skills`
- `github.com/codalotl/codalotl/internal/specmd`
- `github.com/codalotl/codalotl/internal/subscriptions/openaisub`
- `github.com/codalotl/codalotl/internal/tools/authdomain`
- `github.com/codalotl/codalotl/internal/tools/cli`
- `github.com/codalotl/codalotl/internal/tools/coretools`
- `github.com/codalotl/codalotl/internal/tools/exttools`
- `github.com/codalotl/codalotl/internal/tools/pkgtools`
- `github.com/codalotl/codalotl/internal/tools/refactor`
- `github.com/codalotl/codalotl/internal/tools/spectools`
- `github.com/codalotl/codalotl/internal/tools/toolsetinterface`
- `github.com/codalotl/codalotl/internal/tui`
- `github.com/codalotl/codalotl/internal/updatedocs`

### Validation
- After implementation batches are done, run `go test ./...` unless an accepted refactor batch already demonstrates equivalent validation.
- Run full `review` once and `check_spec_conformance({"only_changed": true})` once after all planned implementation work is done.
- Resolve non-latent SPEC conformance issues before completion.

## Review

Not yet run.

## Summary

TBD.

## State

- Branch: `jn/refactor-docs-add-all-packages`
- PR file: `.prs/2026-05-30_1780158779_refactor-docs-add-all-packages.md`
- Current request is broad docs-add refactoring across all Go packages.
- `go list ./...` currently reports 67 packages.
- Clarify CAS records exist for target packages `internal/gocas`, `internal/q/cas`, and `internal/subscriptions/openaisub`, but the clarify doc-improvement refactor found no opportunities for all three packages.
