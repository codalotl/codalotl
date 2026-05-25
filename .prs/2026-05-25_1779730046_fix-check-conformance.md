# PR

## User Summary (do not modify)

grant codalotl_cli access to `codalotl spec status`

Ask the user to restart the TUI. Then use that to detect and verify the spec conformance of all packages that lack conformance (ignore ones without SPEC.md).

For any nonconformance, assess. Either update SPEC.md to match behavior, or fix code to match match spec. Optimize for UX and use good judgement.

## Plan

### Package exposing the `codalotl_cli` whitelist
- Locate the CLI/tool whitelist that backs the `codalotl_cli` tool.
- Add access for `codalotl spec status`.
- Validate the command is exposed by the tool after the TUI restart reloads tool metadata.

### Spec conformance sweep
- After `codalotl spec status` is available, run it to find packages with SPEC.md files that lack current conformance records.
- Ignore packages without SPEC.md.
- For each reported package, assess whether behavior or SPEC.md should change.
- Fix nonconformance with good UX judgement, preferring code fixes when the SPEC.md accurately describes intended behavior and SPEC.md updates when the implementation behavior is the intended contract.

### Final validation
- Run review and changed-package SPEC conformance checks.
- Record outcomes and write the PR summary.

## Review

## Summary

## State

- Active branch: `fix-check-conformance`.
- Active PR file: `.prs/2026-05-25_1779730046_fix-check-conformance.md`.
- Current `codalotl_cli` tool help only exposes `cas recertify`, `docs add`, and `docs fix`; `spec status` is not available in this TUI session yet.
