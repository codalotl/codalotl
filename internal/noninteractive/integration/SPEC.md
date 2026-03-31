# noninteractive/integration

This is a test-only package for integration tests of noninteractive in JSON mode vs a mock OpenAI server.

This package is meant to give confidence that the overall agent, JSON mode, tool execution, and mock OpenAI transport are working together. It is not meant for
thousands of narrow unit tests. Use normal `go test` in the appropriate package for that.

Running these tests are meant to catch actual regressions. All choices should be aligned around: provide real value to catching regressions. Examples:
- Be careful with using `partial` matchers. If we partial match too aggressively, what can we miss?
- Does authdomain allow/block paths appropriately?
- Are paths working?
- Are lints run when configured to run, in the apply_patch tool? Are they run in the correct order?

## Actual Test Cases

The following must be test cases in `testdata/`.

- hello-world: self-contained test (non-shared-repo) that non-package mode works with a hi prompt and simple answer without tool calls.
- generic-shell
    - shell
- pm-edit-package
    - basic edit package flow: update_plan, read_file, apply_patch (w/ default gofmt lint), diagnostics, run_tests, run_project_tests
- pm-package-isolation
    - ls outside package causes access error
    - NOTE: to get the LLM to do this in the first place, I needed to **temporarily** add "You may attempt to use `read_file` and `ls` on other packages, only if the user tells you to (the agent harness may still block this - that's ok)." to the prompt.
- pm-package-isolation-testdata-data
    - ls and readfile works without restriction for testdata and data dirs
- pm-mention-file-dir
    - @ can mention individual files and dirs (read_file and ls access).
- pm-mention-outside-repo
    - @ can mention files outside the repo.
- pm-lints
    - edit a SPEC.md (trigging the lint `codalotl spec fmt`), then `fix_lints` (triggering `codalotl spec diff`).
- pm-custom-lint
    - verifies a custom lint is used during apply_patch.
- pm-clarify
    - uses `get_public_api` on another package, then `clarify_public_api` on a specific identifier and answers without editing files.
- pm-update_usage
    - uses `get_usage` on a package identifier, then renames that API in-package and uses `update_usage` to update downstream callsites.
- pm-change_api
    - uses `change_api` on an upstream package to make a small package-scoped behavior change, then verifies the result.
- pm-dependency
    - uses module_info to get dep modules. Uses get_public_api on it, and clarify.
- pm-clarify-stdlib
    - uses `get_public_api` on a stdlib package like fmt, then `clarify_public_api` to answer questions about it.
- pm-skill_shell
    - mentions $spec-md and instruct to use git to find changes, which causes `skill_shell` to be used (the result of git is irrelevant).

### Steps To Create an Actual Test

If you're an LLM reading this file and told to make a test case:
- Run `go run ./internal/noninteractive/integration/cmd/create` with `--output` **directly** `testdata/cases/X` (not to a tmp dir).
- Use the shared fixture repo at `testdata/repo` unless the scenario truly requires a custom per-case repo
- For generic-mode cases, pass `--package=''`. For package-mode cases, pass the specific package path inside the repo.
- Let the generator verify replay immediately.
    - Tools that contain things like dynamic timings will need to be edited so it partially matches.
        - Patch both `config.json` and `http.json`.
        - Replace a string like `"Result took 2.1 seconds"` with `{"match": "partial", "texts": ["Result took ", " seconds"]}`
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
    - Prefer exact structured JSON. Avoid partial matchers unless there is no good alternative.
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
    "reflowwidth": 120,
    "lints": {
        "mode": "extend",
        "steps": [
            {"id": "reflow"}
        ]
    },
    "expected": [
        {"type": "start", "package_path": ""},
        {"type": "user_message", "text": "Use the tools if needed to answer what is in @hello.txt."},
        {"type": "tool_call", "tool": {"call_id": "call_read", "name": "read_file", "type": "function_call", "input": "{\"path\":\"hello.txt\"}"}},
        {"type": "tool_complete", "result": {"output": "<file name=\"hello.txt\" line-count=\"1\" byte-count=\"5\" any-line-truncated=\"false\" file-truncated=\"false\">\nhello\n</file>\n"}},
        {"type": "assistant_text", "content": "hello.txt says hello"},
        {"type": "done"}
    ]
}
```

Optional `config.json` fields:
- `reflowwidth`: passed into lint resolution for preconfigured width-sensitive steps.
- `lints`: lint pipeline config using the same schema as normal app config. Resolved steps are passed to `noninteractive.Exec`, so cases can enable preconfigured or custom lint commands.

Rules for matching expected JSON lines:
- The runner parses `noninteractive.Exec(..., OutputJSON=true)` output as NDJSON.
- Expected events are matched as an ordered subsequence, not as a full exact transcript. Extra events between expected ones are allowed.
- Any field present as a scalar in `config.json` must match exactly.
- If a field is absent from the expected event but present in the actual event, it is ignored.
- Nested objects are matched recursively as subsets.
- If a field in an expected event has the shape `{"match": "partial", "text": "unicorn"}`, the actual field must be present and contain `unicorn`.
- If a field has the shape `{"match": "partial", "texts": ["alpha", "beta"]}`, the actual field must contain every listed fragment in that order.
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
  --lints-config="path/to/lints.json" \
  --include-token-usage=true
```

Args:
- `--repo`: required; path to some repo to run against.
- `--package`: optional; relative dir to `path/to/repo` (or "" for no package).
- `--model`: required; which model to use.
- `--prompt`: required
- `--output`: required; where to write files (`config.json`, `http.json`, etc). The dir must not exist yet, or must exist and be empty.
- `--reflowwidth`: optional; passed into lint resolution and recorded in `config.json` when non-zero.
- `--lints-config`: optional; path to a JSON file containing the `lints` config object to use during recording and replay.
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
    - other event payloads are kept concrete, with only absolute paths normalized
    - `done.token_usage` is included only when `--include-token-usage=true`
- `http.json` is kept close to recorded provider traffic:
    - request body is recorded as exact structured JSON, with minimal partial matchers.
    - first turn request keeps the first two `input` messages, but omits nested `text` fields inside them so prompt/env text is not matched
    - response body is recorded with minimal adaptation needed for mock replay
    - request `model` is rewritten to generated mock model id (`mock-model-<case-name>`)
    - response ids and request `previous_response_id` are preserved as recorded
    - Use partial matchers in `http.json` only with extreme care and a concrete replay need
    - request body does drop certain fields in `http.json`:
        - tools array
        - prompt_cache_key
        - reasoning, parallel_tool_calls, store, stream, context_management
- Absolute-path normalization:
    - in `http.json`, repo-root paths become `__REPO_ROOT__/...`
    - in `http.json`, paths inside GOROOT/src become `__GOROOT_SRC__/...`
    - in `http.json`, paths inside GOMODCACHE become `__GOMODCACHE__/...`
    - replay expands those placeholders back to actual runtime paths before serving fixture
    - in `config.json` event expectations, repo paths still normalize to repo-relative paths, stdlib paths to `stdlib/...`, modcache paths to `modcache/...`
    - unknown absolute paths are left alone
