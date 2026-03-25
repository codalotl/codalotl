# noninteractive/integration

This is a test-only package for integration tests of noninteractive in JSON mode vs a mock OpenAI server.

This package is meant to give confidence that the overall agent, JSON mode, tool execution, and mock OpenAI transport are working together. It is not meant for
thousands of narrow unit tests. Use normal `go test` in the appropriate package for that.

## Cases

Each test case is found in its own folder in `testdata/`. Example: `testdata/basic-tool-call`.

Structure of a test case folder:
- `config.json` - prompt, package mode settings, and the ordered subsequence of expected JSON events.
- `http.json` - mock OpenAI request/response definitions consumed by `internal/mockllm/mockopenai`.
- `repo/*` - files copied into a temp dir before running `noninteractive.Exec`.
- `expected_repo/*` - optional file snapshots to compare against the temp repo after the run.

## config.json

Example:

```json
{
    "prompt": "Use the tools if needed to answer what is in @hello.txt.",
    "package_path": "",
    "expected": [
        {"type": "start", "package_path": ""},
        {"type": "user_message", "text": "Use the tools if needed to answer what is in @hello.txt."},
        {"type": "tool_call", "tool": {"call_id": "call_read", "name": "read_file", "type": "function_call", "input": "{\"path\":\"hello.txt\"}"}},
        {"type": "tool_complete", "result": {"output": {"match": "partial", "text": "<file name=\"hello.txt\""}}},
        {"type": "assistant_text", "content": "hello.txt says hello"},
        {"type": "done"}
    ]
}
```

Rules for matching expected JSON lines:
- The runner parses `noninteractive.Exec(..., OutputJSON=true)` output as NDJSON.
- Expected events are matched as an ordered subsequence, not as a full exact transcript. Extra events between expected ones are allowed.
- Any field present as a scalar in `config.json` must match exactly.
- If a field is absent from the expected event but present in the actual event, it is ignored.
- Nested objects are matched recursively as subsets.
- If a field in an expected event has the shape `{"match": "partial", "text": "unicorn"}`, the actual field must be present and contain `unicorn`.
- Partial matchers work for both strings and structured JSON values; non-strings are marshaled to JSON before matching.
