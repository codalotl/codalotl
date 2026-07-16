# PR

## User Summary (do not modify)

Make `apply_patch` authorize the exact filesystem paths that it applies.

Authorization is a tool boundary that users and package mode rely on: every add, update, delete, and move must be checked before any part of the patch changes the filesystem. In package mode, this is also what keeps direct edits inside the selected package code unit.

The current implementation determines authorization paths separately from the patch behavior. Those two interpretations do not always agree. Accepted whitespace around operation headers can cause a target to be omitted from authorization, while leading whitespace in a path can cause one path to be authorized and a different path to be applied. As a result, an accepted patch can bypass its intended authorization check or the selected package boundary.

Fix `apply_patch` so every source and destination path it will actually mutate is authorized first, using the same path interpretation as patch application. If any required path is denied, the patch must not modify the filesystem. This must hold for all supported patch operations and accepted patch syntax, including moves and compatibility forms with surrounding whitespace.

## Plan [DONE]

### Package `internal/applypatch` [DONE]

- Add path discovery backed by the same complete parse and path-resolution logic used by patch application.
- Return every resolved source and move destination before filesystem mutation, preserving first-seen order and avoiding duplicate authorization targets.
- Cover accepted compatibility whitespace and all operation types.

### Package `internal/tools/coretools` [DONE]

- Authorize `apply_patch` paths returned by `internal/applypatch` before applying any hunk.
- Remove the independent header scanner and path resolver.
- Verify denial of any source or destination leaves every patch target unchanged.

## Review

- Full review against `main`: no findings; patch correct (confidence 0.95).
- SPEC conformance: `internal/applypatch` and `internal/tools/coretools` conform.
- Validation: focused package tests, `go test ./...`, and `git diff main --check` pass.

## Summary

- Add parser-backed `applypatch.AffectedPaths`, sharing complete parsing and path resolution with `ApplyPatch`.
- Preflight every patch target before mutation and authorize the full unique path set, including move sources and destinations.
- Remove coretools' divergent header scanner and cover compatibility whitespace, intentional path whitespace, aliases, moves, and denied multi-target patches.

Tests:
- `go test ./internal/applypatch ./internal/tools/coretools`
- `go test ./...`
- `git diff main --check`

## State

- Branch: `jn/apply-patch-exact-authorization`, based on `main` at `f07bcdd`.
- Existing bug: `coretools.collectPatchPaths` scans raw lines and trims paths independently, while `applypatch.parsePatch` accepts whitespace around headers and preserves leading path whitespace after the required delimiter.
- Design: add `applypatch.AffectedPaths`; share parsing/resolution preparation with `ApplyPatch`; have coretools authorize its absolute paths before applying.
- Implementation: `0751f24`; focused package tests and `go test ./...` pass.
- Test cleanup: `53059ab` preserves whitespace-sensitive inputs without source-level trailing whitespace.
- Review complete with no findings; changed packages conform to their SPECs.
- PR complete; summary written after review and conformance gates passed.
