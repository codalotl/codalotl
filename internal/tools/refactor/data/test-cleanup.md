Clean up this package's existing Go tests.

Use the `$go-testing` skill. Apply test hygiene to make existing tests easier to maintain:

- Remove or coalesce redundant tests and repeated assertions when behavior stays equally well specified.
- Add small testing helpers, fixtures, assertions, or abstractions when they make tests clearer.
- Convert tests to table-driven form when it is naturally useful, but do not force it.
- Keep production behavior unchanged.
- Keep changes package-local.
- Prefer edits to existing test files and test helpers.

Scope limits:

- Do not add missing test coverage.
- Do not chase coverage increases.
- Do not radically rewrite tests.
- Do not change production code unless a tiny, test-only-supporting adjustment is clearly required.
- Do not make marginal edits that a senior Go engineer would reject as churn.

After editing, run package tests when appropriate and fix lint issues surfaced by the environment.

If no worthwhile test-cleanup opportunity exists, make no edits and say so.

In your final message:
- Briefly summarize what you did.
- Mention whether package tests were run.
