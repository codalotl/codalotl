# Skills

## $spec-md

## $go-testing

- This skill defines best practices for Go tests. Things like:
    - Prefer table-driven tests.
    - Use test helpers.
    - Avoid repetative testing scenarios
    - Use assertion-based testing (`testify`) if it's enabled in the `go.mod`.
        - use require vs assert correctly.
        - don't pass strings to assert helpers unless necessary.
    - What else? ideas:
        - focus on testing the interface, not on private helpers
        - tests shouldn't usually hit external network services
        - don't stub implementation details unless necessary
- It describes certain commands like `go tool cover`, so that they can be used by `skill_shell`.
    - But it reinforces tools like `run_tests` if available.