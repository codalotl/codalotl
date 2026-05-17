---
name: go-testing
description: Guidance for writing and refactoring Go tests. Use when writing, improving, reorganizing, deduplicating, or validating Go tests, including table-driven tests, helpers, testify assertions, package boundary tests, and Go test/coverage commands.
---

# Go Testing

Use this skill for writing and refactoring Go tests.

## Guidance

- Tests are just code. Create and use good abstractions and helpers.
- Prefer table-driven tests when appropriate. These create leverage and reduce the burden to add new cases.
- Use assertion-based testing (e.g., `testify` or similar) if it's enabled in the `go.mod` or used in the package you're working on.
    - Use `assert` vs `require` appropriately.
    - DON'T supply string descriptions in asserts (ex: `assert.True(t, someBool, "dont include this string")`) UNLESS the assert has indirection of some kind where extra context is needed.
- Testing the package's public API is preferrable to testing internal helpers. It affords us the ability to refactor implementation details without affecting tests.
- All else being equal, as a rule of thumb, strive for ~80-90% test coverage.
    - Don't literally measure this with the `cover` tool unless explicitly told to.
    - Rationale: going from 80% -> 100% is 5x more expensive as going from 0% -> 80%, and often forces the implementation into awkward designs to allow for stubbing/mocking.
- Don't over-mock. Full-stack tests actually validate the end-to-end functionality and are very valuable.
    - Exceptions: calling external services are often best mocked.
- If just making a minor tweak to a package, it's often fine to do so without adding a new test.

## Code Coverage

When you need to get code coverage metrics, use commands like these (adapt as needed):
- `go test ./path/to/mypkg -coverprofile=/tmp/mypkg-coverage.out`
- `go tool cover -func=/tmp/mypkg-coverage.out`
- `go tool cover -html=/tmp/mypkg-coverage.out`

NOTE: to just run normal tests, prefer the environment's built-in testing tools (ex: `run_tests`, `run_project_tests`).
