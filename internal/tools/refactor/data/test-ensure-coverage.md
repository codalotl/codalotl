Ensure this package has worthwhile Go test coverage.

Use the `$go-testing` skill. Measure package coverage with `go test -coverprofile` and inspect the results with `go tool cover -func` when useful.

Focus on high-value missing coverage:

- Test public APIs and exported behavior that callers rely on.
- Add coverage for important edge cases, error paths, and boundary conditions.
- Prefer tests that specify meaningful behavior over tests that merely increase the percentage.
- Use table-driven tests, helpers, fixtures, or assertion libraries when they make the tests clearer.
- Keep production behavior unchanged. Only edit tests.

Overall: add maintainable tests that make the package's public behavior and important edge cases better specified.

Scope limits:

- Do not primarily reorganize, rename, or refactor tests; that is handled by `test-cleanup`.
- Do not add low-value tests just to chase 100% coverage.
- Do not edit non-test code, even if it makes writing tests easier.
- Do not make marginal edits that a senior Go engineer would reject as churn. This coverage task is run regularly, so it's very important to avoid churn.

If you find a bug while adding coverage:
- Do not fix it.
- Add a PASSING test case that documents the bug, with a comment on top that starts with `// POSSIBLE BUG:`, describing the bug.

After editing, run package tests when appropriate and fix lint issues surfaced by the environment.

If coverage is already adequate and no worthwhile missing tests exist, make no edits and say so. Again, this is often fine, as this task is run iteratively.

In your final message, briefly summarize what you did, including any coverage measurement you used.
