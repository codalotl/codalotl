# noninteractive/integration

This is a test-only package for integration tests of noninteractive in JSON mode vs a mock OpenAI server.

This package is meant to give confidence that the overall agent, JSON mode, tool execution, and mock OpenAI transport are working together. It is not meant for
thousands of narrow unit tests. Use normal `go test` in the appropriate package for that.

## Actual Test Cases

The following must be test cases in `testdata/`.

- hello-world: self-contained test (non-shared-repo) that non-package mode works with a hi prompt and simple answer without tool calls.
- simple-tool-call-generic-mode:
    - read_file then apply_patch
- simple-tool-call-package-mode:
    - read_file then apply_patch

### Steps To Create an Actual Test

Best practice:
- Prefer generating the case with `go run ./internal/noninteractive/integration/cmd/create` instead of hand-authoring `config.json` or `http.json`.
    - Set `--output` to directly write the test case to `testdata/cases/X`.
- Prefer using the shared fixture repo at `testdata/repo` unless the scenario truly requires a custom per-case repo.
- For generic-mode cases, pass `--package=''`. For package-mode cases, pass the specific package path inside the repo.
- Let the generator verify replay immediately. If generation does not replay cleanly, fix the prompt or scenario first instead of checking in a flaky case.
- After generation, read `config.json`, `http.json`, and every file in `expected_repo/` and confirm they make sense:
  - the tool sequence should match the intended scenario
  - `expected_repo/` should contain exactly the changed or created files
  - there should be no surprising extra reads, writes, or assistant output
- Run `go test ./internal/noninteractive/integration/...` before committing.

Recommended workflow:

```sh
go run ./internal/noninteractive/integration/cmd/create \
  --repo=./internal/noninteractive/integration/testdata/repo \
  --model=gpt-5.4-high \
  --package='' \
  --prompt='Read catalog/query.go, then make the smallest possible change so ProductsWithTag returns nil immediately when tag is empty. Do not run tests or read any other files.' \
  --output=./internal/noninteractive/integration/testdata/cases/simple-tool-call-generic-mode

go test ./internal/noninteractive/integration/...
```

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
- `--output`: required; where to write files (`config.json`, `http.json`, etc). The dir must not exist yet, or must exist and be empty.
- `--include-token-usage`: optional (default false); if "true", includes token usage in `done` event.

Details:
- Copies the repo to a tmp dir, and runs noninteractive on it.
- The CLI prints human-readable progress to stderr while the real agent's NDJSON stream is written to stdout.
- Saves request/responses with `llmstream.AddDiagnosticHook`.
- Diff pre/post workdir snapshots to record filesystem changes.
- Copies the input repo into `output/repo`, UNLESS the input repo is specifically the `testdata/repo` fixture.
- Run the generated case through the existing integration harness immediately to verify replay works.
- `config.json` is normalized rather than recorded verbatim:
  - `start` keeps `type` and `package_path`, but omits `cwd` and `model_id`
  - `agent.id` is omitted; only `agent.depth` is kept
  - `assistant_reasoning` events are omitted, because the mock transport does not replay reasoning deltas
  - `tool_complete.result.output` and `permission.prompt` are recorded as partial matchers
  - `done.token_usage` is included only when `--include-token-usage=true`
- `http.json` is normalized rather than recorded verbatim:
  - request `model` is rewritten to the generated mock model id (`mock-model-<case-name>`)
  - request `input` is matched via a stable partial-text snippet rather than exact full JSON
  - response ids and output item ids are rewritten to deterministic fixture ids
- The generator intentionally aims for replay stability, not perfect redaction. If a recorded tool result or assistant response contains sensitive content, edit the generated files before committing them.
