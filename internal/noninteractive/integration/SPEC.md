# noninteractive/integration

This is a test-only package for integration tests of noninteractive in JSON mode vs a mock OpenAI server.

This package is meant to give confidence that the overall agent, JSON mode, tool execution, and mock OpenAI transport are working together. It is not meant for
thousands of narrow unit tests. Use normal `go test` in the appropriate package for that.

## Fixture repo

A re-usable fixture can be found in `testdata/repo`, which contains multiple Go packages that we can operate on.

## Cases

Each test case is found in its own folder in `testdata/cases`. Example: `testdata/cases/basic-tool-call`.

Structure of a test case folder:
- `config.json` - prompt, package mode settings, and the ordered subsequence of expected JSON events.
- `http.json` - mock OpenAI request/response definitions consumed by `internal/mockllm/mockopenai`.
- `repo/*` - files copied into a temp dir before running `noninteractive.Exec`. If `repo` is not present, we use the shared fixture repo.
- `expected_repo/*` - optional file snapshots to compare against the temp repo after the run.
    - If `expected_repo/` has any files at all, it should contain ALL modified and created files of tmp dir vs the original repo.
        - non-modified files need not be present
        - deleted files are not modeled via `expected_repo`. (We could choose to record them via `config.json` in the future).

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

## Creating test cases

Run:

```sh
go run ./internal/noninteractive/integration/cmd/create \
  --repo=path/to/repo \
  --model="gpt-5.4-high" \
  --package="path/to/pkg" \
  --prompt="fix bug..." \
  --output="path/to/output/dir" \
  --include-token-usage=true
```

Args:
- `--repo`: required; path to some repo to run against.
- `--package`: optional; relative dir to `path/to/repo` (or "" for no package).
- `--model`: required; which model to use.
- `--prompt`: required
- `--output`: required; where to write files (`config.json`, `http.json`, etc).
- `--include-token-usage`: optional (default false); if "true", includes token usage in `done` event.

Details:
- Copies the repo to a tmp dir, and runs noninteractive on it.
- Saves request/responses with `llmstream.AddDiagnosticHook`.
- Diff pre/post workdir snapshots to record filesystem changes.
- Run the generated case through the existing integration harness immediately to verify replay works.
- Requests/Responses need to be sanitized vs recorded verbatim.
