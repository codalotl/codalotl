# PR

## User Summary (do not modify)

Many Go packages in this repo have `SPEC.md` files that act as control planes and specifications for their code. "Checking conformance" means evaluating whether a package's Go code and related package contents actually comply with its `SPEC.md`, so we can trust those specs and track verified conformance state in CAS. We recently added a tool (check_spec_conformance) so that orchestrators everywhere can use to automatically check spec conformance of modified packages, and record that fact in CAS.

Problem:

1. The check conformance subagents' output is not fully validated. For example, if it returns {conforms: false} without listing nonconformances, or {conforms: true, nonconformances: [...]}, both are accepted.
2. The actual nonconformances aren't printed in the final tool output. I'd like them to be.

Goal:
- Validate output of subagent. If invalid, subagent should error.
- user can see actual nonconformances printed out.

## Plan

### Phase 0 [DONE]

#### Package internal/tools/spectools [DONE]
- Tighten `check_spec_conformance` result contract in `internal/tools/spectools/SPEC.md` so invalid subagent JSON combinations fail closed:
  - `{"conforms":true}` must not include non-null `nonconformances`; explicit `null` is acceptable and treated the same as omission
  - `{"conforms":false}` must include at least one nonconformance
- Update completion presentation so non-conforming packages surface actual nonconformance details instead of only counts.
- Implement in `internal/tools/spectools/check_spec_conformance.go` and extend focused unit coverage in `internal/tools/spectools/check_spec_conformance_test.go`.

## Decisions

- User decision: `{"conforms":true,"nonconformances":null}` is harmless and should remain valid. Handle this with a `SPEC.md` change, not an implementation change.

## Review

### `check_spec_conformance` with `only_changed=true` [DONE]

- Initial run flagged `{"conforms":true,"nonconformances":null}` as non-conforming under the earlier spec wording.
- User decided that shape is harmless and should be accepted by spec.
- Updated `internal/tools/spectools/SPEC.md` so explicit `null` is equivalent to omission for `conforms=true`.
- Reran `check_spec_conformance` with `only_changed=true`: `internal/tools/spectools` conforms.

## Summary

## State

- Branch: `jn/check_conformance_solidify`
- Target package: `internal/tools/spectools`
- Impl commit `56cee46` now rejects `conforms=true` payloads that include `nonconformances`, and rejects `conforms=false` payloads without at least one issue
- `conforms=true` with explicit `nonconformances:null` is now intentionally allowed by `internal/tools/spectools/SPEC.md`
- `presentCheckSpecConformanceBody` now renders per-package issue details as `- [severity, new|latent] message`
- Focused coverage added for invalid result shapes and for completion-body issue rendering
- Verified locally: `go test ./internal/tools/spectools`
- After the spec adjustment, `check_spec_conformance` with `only_changed=true` reports `internal/tools/spectools` conforming
