Clean up this package's existing Go tests.

Use the `$go-testing` skill. Apply test hygiene to make existing tests easier to maintain:

- Remove or coalesce redundant tests and repeated assertions when behavior stays equally well specified.
    - This is a big one. AI agents LOVE to add a new test case for any task they do, often unnecessarily.
- Add testing helpers, fixtures, assertions, or abstractions when they make tests clearer.
- Convert tests to table-driven form when it is naturally useful, but do not force it.
- Keep production behavior unchanged. Only edit tests.

Overall: make tests more maintainable and conformant to guidance in `$go-testing`.

Scope limits:

- Do not add missing test coverage or chase coverage increases.
- Do not edit non-test code, even if it makes writing tests easier.
- Do not make marginal edits that a senior Go engineer would reject as churn. This test clean up task is run regularly, so it's very important to avoid churn.

Don't deliberately search for bugs, but if you find one:
- Do not fix it.
- Add a PASSING test case that documents the bug, with a comment on top that starts with `// POSSIBLE BUG:`, describing the bug.

After editing, run package tests when appropriate and fix lint issues surfaced by the environment.

If no worthwhile test-cleanup opportunity exists, make no edits and say so. Again, this is often fine, as this task is run iteratively.

In your final message, briefly summarize what you did.
