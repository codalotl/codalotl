---
name: go-testing
description: Guidance for Go test cleanup and refactoring. Use when improving, reorganizing, deduplicating, or validating Go tests, including table-driven tests, helpers, testify assertions, package boundary tests, and Go test/coverage commands.
---

# Go Testing

Use this skill for Go test cleanup, refactors, and validation. Prefer small, behavior-preserving improvements over broad rewrites.

## Workflow

1. Read existing tests and nearby production code enough to understand behavior and public boundaries.
2. Preserve the test intent and failure signal before changing structure.
3. Prefer dedicated test tools when available (`run_tests`, diagnostics, package test tools). Use shell commands only when those tools are unavailable or insufficient.
4. Run focused tests first, then broader package tests when practical.

## Test shape

- Use table-driven tests when cases share setup, action, and assertions.
- Keep one-off tests as simple standalone tests when a table would hide intent.
- Name cases by behavior, not implementation detail.
- Avoid giant tables with complex per-case callbacks; split tests or add helpers instead.
- Use subtests with `t.Run(tc.name, ...)` when failures need case-level isolation.

## Helpers and fixtures

- Extract helpers for repeated setup, fixture construction, or assertions.
- Keep helpers close to the tests that use them unless they are broadly useful.
- Make helpers fail the test cleanly: accept `*testing.T`, call `t.Helper()`, and use `require` for setup failures.
- Return concrete values from helpers instead of hiding important assertions inside them.
- Avoid over-abstracting test code; duplication is acceptable when it keeps behavior obvious.

## Assertions

- Follow the repository's existing style.
- Use `testify/assert` and `testify/require` when already available or standard for the repo.
- Use `require` for preconditions and setup steps where continuing makes no sense.
- Use `assert` for independent checks that can report multiple failures.
- Do not add assertion message strings unless they add non-obvious context.
- For plain Go assertions, prefer clear failure messages that include got/want values.

## Package and interface boundaries

- Test exported behavior through the public API when possible.
- Use black-box package tests (`package foo_test`) when validating consumer-facing behavior or import cycles are not a problem.
- Use same-package tests (`package foo`) for unexported internals only when that materially improves precision or simplicity.
- Prefer fake implementations at interface boundaries over mocks when behavior is simple.
- Verify edge cases at the boundary where they matter: errors, nil/zero values, empty collections, ordering, and concurrency when relevant.

## Cleanup and refactor guidance

- Coalesce redundant tests only when they assert the same behavior with equivalent signal.
- Keep regression tests for distinct bugs, even if similar to other cases.
- Preserve meaningful test names and comments that explain bug history or non-obvious behavior.
- Avoid adding unrelated missing coverage during cleanup unless the user asks.
- Avoid radical rewrites that make diffs hard to review.

## Useful commands

Prefer dedicated tools exposed by the environment. If shell commands are appropriate:

```bash
go test ./path/to/pkg
go test ./path/to/pkg -run TestName -count=1
go test ./path/to/pkg -cover
go test ./...
go test ./path/to/pkg -coverprofile=coverage.out
go tool cover -func=coverage.out
go tool cover -html=coverage.out
```

Use `-race` when concurrency behavior is relevant and runtime cost is acceptable.
