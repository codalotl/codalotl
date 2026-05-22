# PR

## User Summary (do not modify)

The previous patch was incomplete: .prs/2026-05-20_1779308755_openai-zdr.md

Somehow it was missed that in ZDR mode, we need to re-send encrypted reasoning content. Fix this.

Implementing this is (probably) the easy-ish part. The hard part is validating it works. You (AIs) have a tendancy to just write code, see if tests pass, and call it a day. That won't work here. You must actually test this, with more than a single trivial request, that the real OpenAI actually works. Moreover, validate that input caching is working properly:
- The env has the real openai keys. Use them. Don't worry about sending live traffic to openai.
- make integration tests (see INTEGRATION_TEST) that exercise this.
- Test this with something like `go run . exec` with $CODALOTL_ZDR=true
- make sure caching w/ resending encrypted reasoning fully works.

## Plan

### Phase 0: design/spec [DONE]

#### Package `internal/llmstream` [DONE]
- Clarify OpenAI `SendOptions.NoStore` semantics so ZDR requests ask OpenAI to return encrypted reasoning content.
- Prior no-store assistant turns must replay encrypted reasoning content in subsequent stateless requests.
- Continue omitting provider output item IDs and reasoning state that requires stored OpenAI server state.
- Keep full-history replay and prompt-cache validation requirements from the previous ZDR PR.

### Phase 1: implementation

#### Package `internal/llmstream`
- Implement the updated `internal/llmstream/SPEC.md`.
- Likely changes:
  - Add `reasoning.encrypted_content` to OpenAI Responses requests when `NoStore` is enabled.
  - Capture encrypted reasoning content from OpenAI reasoning output items.
  - Retain encrypted reasoning state locally on no-store assistant turns while still scrubbing unsafe provider IDs/state.
  - Re-send encrypted reasoning items on subsequent no-store requests.
  - Update request-shape/unit tests and live `INTEGRATION_TEST` coverage so the second or later no-store request contains encrypted reasoning content and still has cached input.

### Phase 2: validation

- Run focused `internal/llmstream` tests.
- Run live OpenAI integration tests with `INTEGRATION_TEST=1`, using a multi-turn no-store reasoning flow that exercises encrypted reasoning replay and cached input.
- Run a live command similar to `CODALOTL_ZDR=true go run . exec --yes --model <openai-reasoning-model> ...` and inspect diagnostic output for:
  - all requests use `store=false`;
  - no `previous_response_id`;
  - encrypted reasoning content is requested and replayed after the first turn;
  - later turns report cached input tokens.

## Review

Not yet run.

## Summary

TBD.

## State

- Branch: `jn/zdr-encrypted-reasoning`.
- PR file: `.prs/2026-05-22_1779467722_zdr-encrypted-reasoning.md`.
- This PR follows up `.prs/2026-05-20_1779308755_openai-zdr.md`.
- Previous ZDR implementation intentionally scrubbed OpenAI no-store reasoning entirely because plain reasoning item IDs require stored server state.
- OpenAI Responses supports `include: ["reasoning.encrypted_content"]`; SDK docs say this enables reasoning items in stateless/ZDR conversations.
- Current target package: `internal/llmstream`.
- Relevant files:
  - `internal/llmstream/SPEC.md`
  - `internal/llmstream/open_ai_responses.go`
  - `internal/llmstream/open_ai_responses_test.go`
  - `internal/llmstream/open_ai_integration_test.go`
- Expected representation: likely use `ReasoningContent.ProviderState` for OpenAI encrypted reasoning content; keep visible reasoning summary/content out of no-store retained turns unless safe/needed.
