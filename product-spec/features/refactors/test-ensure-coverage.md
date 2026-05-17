# Test Cleanup Refactor

Codalotl has a subcommand in the `refactor` tool to ensure test coverage is adequate: `test-ensure-coverage`.
- This is a package-mode subagent.
- The subagent has access to `$go-testing`, an always-available skill. The skill defines best practices, as well as supplying commands for `skill_shell` to run things like `go test -coverprofile` and `go tool cover`.
- This refactor is intended to be able to run regularly.
- Top things it's intended to do:
     - Ensure the public API of a package is tested.
     - Ensure test coverage is adequate.
     - Adds coverage for edge cases.
- It's intended to be run after, and supplement, `test-cleanup`.
- It does NOT primarily refactor test.
- It saves its results in a CAS record.
