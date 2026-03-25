# noninteractive/integration

This is a test-only package for integration tests of noninteractive in JSON mode vs a mock OpenAI server.

This package is meant to give confidence that the overall agent and tool system is working. It is not meant for thousands of narrow unit tests. Use normal `go test` in the appropriate package for that.

## Cases

Each test case is found in its own folder in `testdata/`. Example: `testdata/simple-edit-file`.

Structure of a test case folder:
- `config.json` - contains expected output and any other config overrides.
- `http.json` - the requests/responses for the mock server.
- `repo/*` - the files that the agent is run against (copied to tmp dir, which will be the sandbox dir).
- `expected_repo/*` - Optional. For any given file present here (in any nested folder), we assert that the tmp dir's version matches.

## config.json

Example:

```json
{
    "model": "gpt-5.4-high",
    "expected": [
        {"type": "start", "cwd": "/some/path", "package_path": "internal/somepkg", "model_id": "gpt-5.4-high"},
        {"type": "user_message", "text": "fix failing test"},
        {"type": "tool_call", "tool": {"call_id": "call_1", "name": "read_file", "type": "function_call", "input": "{\"path\":\"foo.go\"}"}}
    ]
}
```

Rules for matching expected JSON lines:
- Any field present as a simple string in the config.json must match exactly.
- If a field is missing from the expected JSON line, but present in the actual, ignore (but the opposite is obviously not true).
- If a field in an expected JSON line has the shape `{"match": "partial", "text": "unicorn"}`, then the actual field must be present and contain "unicorn".