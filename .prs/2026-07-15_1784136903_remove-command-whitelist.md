# PR

## User Summary (do not modify)

See product-spec/features/permissions.md

The current product impl has a concept of command whitelist/blacklist, and things like inscruitible commands, etc. I want to remove that to align with the spec.

## Plan

### Package `internal/tools/authdomain` [DONE]

- Remove shell-command classification, matcher lists, and related public APIs.
- Authorize shell execution using only sandbox working-directory policy and explicit permission requests:
  - strict sandbox denies an outside-sandbox cwd;
  - permissive sandbox prompts for an outside-sandbox cwd;
  - either prompts when the tool explicitly requests permission;
  - otherwise shell execution is allowed without inspecting command argv.
- Simplify authorizer constructors so callers no longer supply shell-command policy.
- Update focused authorization tests and delete obsolete classifier tests.

### Callsites [DONE]

- Update TUI, noninteractive, skills, and test callsites for simplified authorizer constructors.
- Run package and project tests, including replay-backed noninteractive integration tests.
- Validation passed: `go test ./...`; focused authdomain, coretools, skills, noninteractive, and TUI tests also pass.

## Review

Pending.

## Summary

Pending.

## Decisions

- This PR removes shell argv classification from `authdomain`. It does not broaden the `codalotl_cli` tool's intentionally selected in-process command tree, which controls the product APIs exposed as that tool rather than classifying shell commands.

## State

- Planning, SPEC.md review, and implementation complete; review is next.
- Primary package: `internal/tools/authdomain`.
- Existing `coretools` shell execution already states that it has no allowlist/blacklist and delegates authorization to `authdomain`.
- Implementation commit: `3a26edf`.
