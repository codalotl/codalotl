# Skills

## $spec-md

## $go-testing

- This skill defines best practices for Go tests. Things like:
    - Prefer table-driven tests. Use test helpers - test code is still code.
    - Use assertion-based testing (`testify`) if it's enabled in the `go.mod`.
    - Focus on testing interface boundaries.
- It describes certain commands like `go tool cover`, so that they can be used by `skill_shell`.
    - But it reinforces tools like `run_tests` if available.