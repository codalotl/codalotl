# Test Cleanup Refactor

Codalotl has a subcommand in the `refactor` tool to clean up test code: `test-cleanup`.
- This is a package-mode subagent.
- The subagent has access to `$go-testing`, an always-available skill. The skill defines best practices, as well as supplying commands for `skill_shell` to run things like `go test -coverprofile` and `go tool cover`.
- This refactor is intended to be able to run regularly.
- Top things it's intended to do:
     - Remove and/or coalesce redundant tests.
     - Increase test maintainability.
     - Add testing helpers/abstractions.
- It's NOT intended to add missing tests.
- It's NOT intended to radically refactor tests. Instead, its meant to simply apply some hygiene to existing tests.
- It WEAKLY converts tests to table-driven. Weakly meaning: not strongly prompted, but not prohibited.
- It saves its results in CAS record.
