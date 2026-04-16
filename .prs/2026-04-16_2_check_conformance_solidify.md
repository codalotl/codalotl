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

### Phase 0

#### Package internal/tools/spectools
- Tighten `check_spec_conformance` result contract in `internal/tools/spectools/SPEC.md` so invalid subagent JSON combinations fail closed:
  - `{"conforms":true}` must not include `nonconformances`
  - `{"conforms":false}` must include at least one nonconformance
- Update completion presentation so non-conforming packages surface actual nonconformance details instead of only counts.
- Implement in `internal/tools/spectools/check_spec_conformance.go` and extend focused unit coverage in `internal/tools/spectools/check_spec_conformance_test.go`.
- Follow-up from conformance check: reject explicit `{"conforms":true,"nonconformances":null}` the same way as other invalid `conforms=true` shapes.

## Review

### `check_spec_conformance` with `only_changed=true`

- Result: `internal/tools/spectools` is currently non-conforming.
- Reported nonconformance:
  - `minor`, `latent=false`: `parsePackageCheckResult` still accepts `{"conforms":true,"nonconformances":null}` and normalizes it to a conforming result, but the SPEC requires any `conforms=true` result to omit `nonconformances` entirely and treat any other shape as a package-scoped error.

## Summary

## State

- Branch: `jn/check_conformance_solidify`
- Target package: `internal/tools/spectools`
- Impl commit `56cee46` now rejects `conforms=true` payloads that include `nonconformances`, and rejects `conforms=false` payloads without at least one issue
- `presentCheckSpecConformanceBody` now renders per-package issue details as `- [severity, new|latent] message`
- Focused coverage added for invalid result shapes and for completion-body issue rendering
- Verified locally: `go test ./internal/tools/spectools`
- Conformance re-check with `only_changed=true` still reports one remaining gap: explicit `nonconformances:null` is accepted for `conforms=true`
