run_tests runs `go test` in a package.
- Use `test_name` to run only only one test (`go test -run`).
- Use `verbose` to see verbose test output (`go test -v`). Great for debugging failing tests.
- Use `env` to set custom env variables during a test run (for instance: some tests are gated on an env var being set).
- After running tests, it runs any configured linters.
