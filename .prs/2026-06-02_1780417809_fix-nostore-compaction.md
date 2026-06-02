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

### Package `internal/llmstream`

- Implement OpenAI no-store server-side compaction as retained opaque state, per `internal/llmstream/SPEC.md`.
- Add a sealed content part for compaction state, preserve it from completed Responses output and streaming output-item events, and scrub provider IDs in no-store retained turns.
- Replay compaction state as an OpenAI Responses input item during `SendOptions.NoStore` stateless request building.
- When the latest compaction state is present, build no-store replay input from that compaction item forward instead of replaying earlier local history.
- Keep existing stored-mode `previous_response_id` behavior unchanged.
- Add focused unit/request-shape coverage and an OpenAI integration test that exercises no-store compaction replay.

### Validation

- Run `go test ./internal/llmstream`.
- Run relevant integration tests with `INTEGRATION_TEST=1` and OpenAI credentials when available.
- Validate manually with `go run . exec`, temporarily forcing a low compaction threshold if needed to observe compaction.

## Review

## Summary

## State

- Active branch: `jn/fix-nostore-compaction`.
- PR file: `.prs/2026-06-02_1780417809_fix-nostore-compaction.md`.
- Bug is in `internal/llmstream` OpenAI Responses no-store path.
- Current code enables `context_management` for OpenAI autocompaction models, then sets `store=false` for no-store.
- Existing no-store support already avoids `previous_response_id`, replays visible history, and replays encrypted reasoning state.
- Missing support: output item `type:"compaction"` is dropped; request building has no compaction input item; no-store history is not pruned from latest compaction.
