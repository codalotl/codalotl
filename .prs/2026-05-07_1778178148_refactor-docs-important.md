# PR

## User Summary (do not modify)

make the refactor(docs-add) tool use --important, not --public-api

## Plan

### Package internal/tools/refactor [DONE]
- Update `docs-add` refactor to delegate to `codalotl docs add --important <package>`.
- Keep status/result behavior unchanged.
- Update tests and tool description expectations for important-doc behavior.

## Review

- Overall `review()` not run; user requested skipping it.
- `go test ./internal/tools/refactor`: pass.
- `check_spec_conformance` for `internal/tools/refactor`: conforms.

## Summary

- Changed `refactor(docs-add)` to delegate to `codalotl docs add --important <package>`.
- Updated `internal/tools/refactor` SPEC and tests to match important-doc behavior.
- Preserved existing result/status/edit detection behavior.

## State

- Active branch: `jn/refactor-docs-important`.
- Active PR file: `.prs/2026-05-07_1778178148_refactor-docs-important.md`.
- Target package: `internal/tools/refactor`.
- `internal/tools/refactor/SPEC.md` now requires `docs-add` to delegate with `--important`.
- Validation done: package tests pass; SPEC conformance passes for target package.
