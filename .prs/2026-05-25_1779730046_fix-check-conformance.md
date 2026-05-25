# PR

## User Summary (do not modify)

grant codalotl_cli access to `codalotl spec status`

Ask the user to restart the TUI. Then use that to detect and verify the spec conformance of all packages that lack conformance (ignore ones without SPEC.md).

For any nonconformance, assess. Either update SPEC.md to match behavior, or fix code to match match spec. Optimize for UX and use good judgement.

## Plan

### Package exposing the `codalotl_cli` whitelist [DONE]
- Locate the CLI/tool whitelist that backs the `codalotl_cli` tool.
- Add access for `codalotl spec status`.
- Validate the command is exposed by the tool after the TUI restart reloads tool metadata.

### Spec conformance sweep [DONE]
- After `codalotl spec status` is available, run it to find packages with SPEC.md files that lack current conformance records.
- Ignore packages without SPEC.md.
- For each reported package, assess whether behavior or SPEC.md should change.
- Fix nonconformance with good UX judgement, preferring code fixes when the SPEC.md accurately describes intended behavior and SPEC.md updates when the implementation behavior is the intended contract.

### Conformance follow-ups
- [DONE] `internal/q/cas`: fix code. Reject `.` and `..` path segments for namespaces and custom hashes so records cannot escape `DB.AbsRoot`.
- [DONE] `internal/skills`: fix code. `Skill.Validate` should reject the literal `Name` value when it has leading/trailing whitespace; `LoadSkill` can remain forgiving by trimming YAML input before validation.
- [DONE] `internal/gocas`: fix code. Make prior-namespace-version pruning skip corrupt/unrecognized record-shaped files unless it can validate them as CAS records.

### CAS/doc follow-up [DONE]
- Ran `docs-improve-from-clarify` for clarify-public-api records produced by the sweep.
- Accepted documentation-only updates in `internal/skills`, `internal/tools/cli`, and `internal/tools/toolsetinterface`.
- `internal/skills` and `internal/tools/cli` SPEC.md edits were reviewed with `review_spec_changes`; no revisions required.

### Final validation
- Run review and changed-package SPEC conformance checks.
- Record outcomes and write the PR summary.

### Review follow-ups
- [DONE] Fix review finding in `internal/gocas`: prior-version pruning should not follow symlinks or read non-regular record-shaped entries while validating records.

## Review

- Reviewed against `origin/fix-check-conformance`.
- Review finding:
  - P2 `internal/gocas`: `pruneNamespaceRecordFiles` validates record-shaped paths with `readFullCASRecordFile`, which uses `os.ReadFile` and follows symlinks. A malicious or malformed symlink such as `oldns/ab/cd -> /dev/zero` could hang or consume unbounded memory during prune. Fix by checking `DirEntry`/`Info` for a regular file, or otherwise avoiding symlink-following reads before validation.
- Changed-package SPEC conformance: `check_spec_conformance({"only_changed":true})` returned no nonconformances and produced no CAS changes for this tree state.

## Summary

## State

- Active branch: `fix-check-conformance`.
- Active PR file: `.prs/2026-05-25_1779730046_fix-check-conformance.md`.
- Implemented in `internal/cli`: `newCodalotlCLICommandTree` now exposes `spec status` only from the `spec` group.
- Focused validation passed: `go test ./internal/cli` and `go test -run TestCodalotlCLITool_OnlyExposesWhitelistedCommands ./internal/cli`.
- TUI/tool metadata restart completed; `codalotl_cli` now exposes `spec status`.
- `codalotl spec status` initially reported unset SPEC-bearing packages: `internal/cli`, `internal/gocas`, `internal/llmmodel`, `internal/q/cas`, `internal/skills`, `internal/tools/authdomain`, `internal/tools/refactor`.
- `check_spec_conformance` certified `internal/cli`, `internal/llmmodel`, `internal/tools/authdomain`, `internal/tools/refactor`; `internal/tools/cli` was re-certified after doc CAS updates.
- Remaining conformance findings are latent but actionable: `internal/gocas`, `internal/q/cas`, `internal/skills`.
- Fixed `internal/q/cas` dot-segment validation for namespaces and derived hash path segments. Validation passed: `go test ./internal/q/cas`; `check_spec_conformance` certified `internal/q/cas`.
- Fixed `internal/skills` literal name validation while preserving `LoadSkill` frontmatter trimming. Validation passed: `go test ./internal/skills`; `check_spec_conformance` certified `internal/skills`.
- Fixed `internal/gocas` prior-version pruning to delete only validated CAS record files and skip corrupt/unrecognized records. Validation passed: `go test ./internal/gocas`; `check_spec_conformance` certified `internal/gocas`.
- Final validation run: review reported one actionable `internal/gocas` symlink/non-regular-file pruning issue; changed-package SPEC conformance returned no issues.
- Fixed `internal/gocas` review follow-up by skipping non-regular record-shaped entries before reading record contents during prior-version pruning. Validation passed: `go test ./internal/gocas`; `check_spec_conformance` certified `internal/gocas`.
