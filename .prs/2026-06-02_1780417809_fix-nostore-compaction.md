# PR

## User Summary (do not modify)

› consider the server side compaction for openai using context_management. Inspect internal/llmstream. When nostore=true, are we correctly dealing with the compaction?

No. For NoStore=true, we ask OpenAI to do server-side compaction, but we do not preserve or replay the returned compaction item.

  The bug path is:

  - internal/llmstream/open_ai_responses.go:289 enables context_management for OpenAI autocompaction models before no-store handling.
  - internal/llmstream/open_ai_responses.go:330 then sets store=false.
  - OpenAI docs say server-side compaction with store=false is ZDR-friendly, but the response stream includes an encrypted compaction item, and stateless chaining must append output items including compaction items into the next
    input window. Source: OpenAI compaction guide, lines 703-717. https://developers.openai.com/api/docs/guides/compaction
  - Our response mapper only handles function_call, custom_tool_call, message, and reasoning; it drops type:"compaction" output items: internal/llmstream/open_ai_responses.go:900.
  - Our request builder can replay encrypted reasoning but has no compaction replay path: internal/llmstream/open_ai_responses.go:785.

  So stored mode is probably okay via previous_response_id, but no-store stateless mode is incomplete. We either need to disable context_management when NoStore=true, or properly model compaction as a retained opaque content part,
  include it in subsequent input via OpenAI’s compaction input item, and ideally prune local history before the latest compaction item to get the intended token reduction. I did not make code changes or run tests.

Your goal: fix this bug.

NOTE: you can probabably read that page with curl.

Validation:
- in addition to normal tests,
- run the integration tests (see INTEGRATION_TEST env var or something like that)
- Ensure this codepath is exercised and works properly with an actual integration test.
- Also validate it using ~ `go run . exec`. Make sure compaction is actually triggered during that.
- For the above cases, you may temporarily alter the threshold limits (you could also solve it other ways)

## Plan

### Package `internal/llmstream` [DONE]

- [DONE] Implement OpenAI no-store server-side compaction as retained opaque state, per `internal/llmstream/SPEC.md`.
- [DONE] Add a sealed content part for compaction state, preserve it from completed Responses output and streaming output-item events, and scrub provider IDs in no-store retained turns.
- [DONE] Replay compaction state as an OpenAI Responses input item during `SendOptions.NoStore` stateless request building.
- [DONE] When the latest compaction state is present, build no-store replay input from that compaction item forward instead of replaying earlier local history.
- [DONE] Keep existing stored-mode `previous_response_id` behavior unchanged.
- [DONE] Add focused unit/request-shape coverage and an OpenAI integration test that exercises no-store compaction replay.

### Validation

- [DONE] Run `go test ./internal/llmstream`.
- [DONE] Run `go test ./...`.
- [DONE] Run focused mock/request-shape compaction tests.
- Attempt relevant OpenAI integration tests with `INTEGRATION_TEST=1` and OpenAI credentials when available.
- Validate manually with `go run . exec`, temporarily forcing a low compaction threshold if needed to observe compaction.

### Review follow-up

- [DONE] Fix review finding: streamed `response.output_item.done` compaction can be lost when final `response.completed` has non-empty output, because completed output parts replace streamed parts instead of merging retained compaction state.
- [DONE] Fix review finding: streamed-only compaction must preserve provider output order when merged with non-empty completed output, otherwise no-store pruning can drop post-compaction assistant content.

## Review

Validation pass on 2026-06-02:

- `go test ./...` passed.
- `go test -count=1 ./internal/llmstream -run 'TestSendAsyncOpenAIResponses_NoStoreReplaysCompactionWithMockServer|TestBuildOpenAIResponsesRequestParams_NoStoreReplaysLatestCompactionAndPrunesHistory'` passed.
- `INTEGRATION_TEST=1 go test -count=1 -v ./internal/llmstream -run TestOpenAIResponsesProvider_NoStoreZDR` skipped both targeted OpenAI tests because `OPENAI_API_KEY` is unset in this environment.
- Manual `go run . exec` compaction validation not completed; actual OpenAI credential/low-threshold run still needed.
- `review` against `main`: patch incorrect. Finding: preserve streamed compaction with non-empty completions in `internal/llmstream/open_ai_responses.go`.
- `check_spec_conformance({"only_changed":true})`: `internal/llmstream` conforms.

Review follow-up implementation on 2026-06-02:

- Commit `2b9ec76` preserves streamed-only compaction state when completed output is non-empty, while keeping completed output authoritative for message/tool/reasoning parts.
- Added regression coverage for a streamed compaction item followed by non-empty completed output without compaction.
- `go test -count=1 ./internal/llmstream` passed.
- `go test ./...` passed.

Validation pass after review follow-up on 2026-06-02:

- `review` against `main`: patch incorrect. Finding: preserve streamed compaction output order. Current merge appends streamed-only compaction after completed parts, which can make later no-store latest-compaction pruning drop assistant content that actually followed compaction.
- `check_spec_conformance({"only_changed":true})`: `internal/llmstream` conforms.

Decision:

- Review finding is actionable. Fix by retaining streamed output ordering metadata for compaction state so merged completed turns place streamed-only compaction at the provider output position relative to completed output items.

Ordered merge follow-up implementation on 2026-06-02:

- Commit `9baca8d` retains streamed output-index metadata and inserts streamed-only compaction state at the correct relative position among completed output parts.
- Added regression coverage for compaction streamed before a completed message, and no-store replay coverage proving post-compaction message content remains in the next request.
- `go test -count=1 ./internal/llmstream` passed.
- `go test ./...` passed.

Final validation pass on 2026-06-02:

- `review` against `main`: patch correct; no findings.
- `check_spec_conformance({"only_changed":true})`: `internal/llmstream` conforms.

## Summary

## State

- Active branch: `jn/fix-nostore-compaction`.
- PR file: `.prs/2026-06-02_1780417809_fix-nostore-compaction.md`.
- Bug is in `internal/llmstream` OpenAI Responses no-store path.
- Current code enables `context_management` for OpenAI autocompaction models, then sets `store=false` for no-store.
- Existing no-store support already avoids `previous_response_id`, replays visible history, and replays encrypted reasoning state.
- Missing support: output item `type:"compaction"` is dropped; request building has no compaction input item; no-store history is not pruned from latest compaction.
- Implementation commit `2316747` adds `CompactionContent`, captures/scrubs/replays OpenAI compaction items, prunes no-store replay before latest compaction, and adds mock/request-shape coverage.
- Validation run this step: `go test -count=1 ./internal/llmstream` passed.
- Validation found an actionable review issue: streamed compaction state can be dropped if completed response output is non-empty. Next step should fix this before final validation.
- Review follow-up commit `2b9ec76` fixes streamed compaction preservation and passed `go test -count=1 ./internal/llmstream` plus `go test ./...`. Next step should rerun `review` and `check_spec_conformance({"only_changed":true})`.
- Latest validation found a second actionable review issue: streamed-only compaction must preserve provider output order when merged with non-empty completed output, otherwise no-store pruning can drop post-compaction assistant content. Next step should fix ordering, then rerun review/conformance.
- Ordered merge follow-up commit `9baca8d` fixes provider output ordering for streamed-only compaction and passed `go test -count=1 ./internal/llmstream` plus `go test ./...`. Next step should rerun `review` and `check_spec_conformance({"only_changed":true})`.
- Final validation: review passed with no findings; `internal/llmstream` SPEC conformance passed. OpenAI credential-dependent integration/manual exec validation remains not run in this environment.
