# PR

## User Summary (do not modify)

Problem:
- In internal/tui and internal/noninteractive, there's features that rely on subagent final message handling - notably, formatters for the final message (sometimes JSON, which needs to be parsed; sometimes hidden)
    - See llmstream.SubagentFinalMessagePresenter
- Determining the final message is annoying an error prone. and since there's multiple clients (tui vs noninteractive), the functionality is duplicated.
    - Example of annoying: assistant text messages come in parts. adjactent parts may (in theory) need to be combined to form one message
    - Also there can be multiple messages per turn. an assistant text, followed by a thinking blob, then another assistant text, etc.
    - This is doubly annoying because **in practice**, none of this actually happens. messages are not split. But according to the API docs' object models, it "could". And it's super rare for more than one assistant message in a turn.

In this PR:
- Move final message handling to agent
- Use in tui/noninteractive, removing the messy code from there.
- Make agent mental model: an assistant text is NOT a part - it's a full message
- agent will need to hold on to assistant text events until the next event comes in

## Plan

### Phase 0

In this phase, land the agent-owned assistant-message contract, then migrate `internal/tui` and `internal/noninteractive` to trust it.

#### Package `internal/agent` [DONE]

- Update `internal/agent/SPEC.md` for assistant-message normalization and assistant-text finality.
- Implement the event buffering/finality rules below, including per-agent isolation and shutdown flushing.
- Update `CollectFinalAssistantText` to rely on final flagged events plus top-level `done_success`.

#### Package `internal/tui` [DONE]

- Update `internal/tui/SPEC.md`.
- Remove local descendant final-message reconstruction from `internal/tui/tui.go`.
- Use descendant `assistant_text` events with `AssistantTextFinal=true` to drive `llmstream.SubagentFinalMessagePresenter`.
- Render non-final descendant assistant text literally.

#### Package `internal/noninteractive` [DONE]

- Update `internal/noninteractive/SPEC.md`.
- Remove local descendant final-message reconstruction from `internal/noninteractive/session.go`.
- Use final flagged events for descendant final-message presentation and for `Result.FinalAssistantText`.
- Keep human-readable and JSON output stable unless a targeted change is required by the new contract.

#### Validation

- Package tests for `internal/agent`, `internal/tui`, and `internal/noninteractive`
- Re-run affected noninteractive integration coverage if request/event shapes intentionally change
- Review plus changed-package SPEC conformance after implementation

#### Downstream follow-up

- [DONE] Update downstream tests/helpers in `internal/agentbuilder` that still model the old final-assistant-text contract.
- [DONE] Update downstream tests/helpers in `internal/tools/pkgtools` that still model the old final-assistant-text contract.
- Those packages call `CollectFinalAssistantText` and now need mocked event streams that include final-flagged `assistant_text` plus top-level `done_success`.

### Design details

Make `internal/agent` the single owner of "which assistant text was the final message for this agent run?" logic. `internal/llmstream` can keep its current provider-shaped/event-part model; `agent` should normalize that into a simpler event contract that downstream consumers can trust.

#### Proposed event contract

- Keep `EventTypeAssistantText`, but define it as an agent-level assistant message event, not a raw provider text-part event.
- This is an intentional normalization boundary:
    - `internal/llmstream` may continue to expose multiple text parts within one assistant turn.
    - `internal/agent` is responsible for coalescing those parts into one or more non-split assistant messages before emitting `EventTypeAssistantText`.
- Add a finality bit on `agent.Event`, only meaningful for `EventTypeAssistantText`.
    - Example shape: `AssistantTextFinal bool`
    - If a less ad-hoc API feels better, use an enum instead, but a bool is probably enough for this PR.
- `AssistantTextFinal=true` means: this is the last assistant text emitted by this agent before that same agent emits a terminal event (`done_success`, `error`, or `canceled`).
- `AssistantTextFinal=false` means: this assistant text was followed by some later event from the same agent, so it is not the terminal assistant message for that run.
- Finality is per-agent. Parent and child agents must not affect each other's finality bookkeeping.

#### Agent buffering rules

- The agent keeps at most one pending assistant-message buffer per agent ID.
- The pending buffer is string content plus enough metadata to emit one `EventTypeAssistantText` later.
- When the provider produces a completed text block, the agent does not emit it immediately. Instead:
    - If there is no pending assistant-message buffer for that agent, start one with that text.
    - If there is already a pending assistant-message buffer for that agent, append the new text to that same buffer.
- Adjacent provider text blocks from the same agent belong to the same agent-level assistant message.
- A non-text event from that same agent is a message boundary. It resolves the pending assistant-message buffer:
    - If the boundary event is `done_success`, `error`, or `canceled`, emit the buffered assistant message with `AssistantTextFinal=true`, then emit the terminal event.
    - For any other boundary event, emit the buffered assistant message with `AssistantTextFinal=false`, then emit the boundary event.
- This means:
    - `assistant_text`, `assistant_text`, `done_success` becomes one final `assistant_text`.
    - `assistant_text`, `assistant_text`, `tool_call` becomes one non-final `assistant_text`, then `tool_call`.
    - `assistant_text`, `assistant_reasoning`, `assistant_text`, `done_success` becomes two assistant-text messages: the first non-final, the second final.
- `AssistantTurnComplete` does not by itself create a new assistant-text message. It is only a completed-turn marker and should not split or flush buffered assistant text on its own.
- Descendant events do not resolve an ancestor's pending assistant text, and ancestor events do not resolve a descendant's.
- On channel close / agent shutdown, there must not be an unclassified pending assistant text. The agent should always flush it as final or non-final before closing the stream.

#### Meaning of "final"

- "Final" is about stream position, not success.
- The final assistant text may be followed by:
    - `done_success`: successful final answer
    - `error`: last assistant text before failure
    - `canceled`: last assistant text before cancellation
- Consumers that need "successful answer text" must still require a later `done_success`; `AssistantTextFinal=true` alone is not enough to imply success.
- Consumers that only care about presentation policy for the last message can use the finality bit without re-deriving anything from turn history.

#### Consumer rules

- `internal/tui` and `internal/noninteractive` should stop reconstructing "the final message" by buffering descendant assistant text themselves.
- They should rely on `EventTypeAssistantText` plus the finality bit.
- `SubagentFinalMessagePresenter` should run only for descendant `assistant_text` events where `AssistantTextFinal=true` and the message falls under the active tool scope.
- Non-final descendant assistant text should always be rendered literally.
- Hidden/fancy-formatted final descendant messages become a pure presentation decision again, not a stream-reconstruction problem.
- `CollectFinalAssistantText` should become a thin helper over the flagged events:
    - remember the last `assistant_text` with `AssistantTextFinal=true` for the target agent
    - return it only if the target agent later emits `done_success`
    - ignore descendant terminal events as it already does today
- `AssistantTurnComplete` should remain available for conversation history, token/context accounting, and any caller that needs the whole turn, but it should no longer be the mechanism for "what was the final assistant message?"

#### Deliberate simplifications / non-goals

- We are intentionally normalizing at the `agent` layer. We do not need TUI/noninteractive to preserve provider-level text-part fidelity.
- We do merge adjacent provider text blocks into one assistant message.
- We do not merge assistant text across explicit non-text boundaries such as reasoning, tool calls, queued user-message markers, retries/warnings, or terminal events.
- We do not try to retroactively edit already-emitted events.
- We do not keep duplicate "legacy" final-message reconstruction code in TUI/noninteractive once the agent contract exists.

#### Caveat / possible correction to the User section

- There is no conflict between:
    - providers emitting multiple text blocks in one assistant turn, and
    - `agent.EventTypeAssistantText` meaning "one non-split assistant message".
- The design simply makes `agent` the normalization layer that coalesces adjacent provider text blocks into message-shaped events for downstream consumers.

## Review

- 2026-04-19 out-of-band review item: [P1] synthesize `assistant_text` from completed turns when no text deltas arrive. [DONE]
  - Assessment: actionable; I agree with the review item.
  - Why:
    - `internal/agent/sendOnce` currently only creates `EventTypeAssistantText` from `llmstream.EventTypeTextDelta` events where `Done=true` (`internal/agent/agent.go:309-316`).
    - On `llmstream.EventTypeCompletedSuccess`, it only stores `ev.Turn` (`internal/agent/agent.go:331-332`), and later emits `EventTypeAssistantTurnComplete` from that completed turn (`internal/agent/agent.go:361-362`).
    - `internal/llmstream` documents `EventTypeTextDelta` as something that "may be emitted", while `EventTypeCompletedSuccess` carries the final `Turn`, so a provider or test double can legally put assistant text only in `Turn.Parts`.
    - In that case, `pendingAssistantText` stays empty, so the run emits `assistant_turn_complete` and terminal status but never emits `assistant_text`.
  - Impact:
    - `agent.CollectFinalAssistantText` can return an empty answer on successful runs.
    - `internal/noninteractive.Result.FinalAssistantText` can lose the final answer.
    - descendant final-message presentation in `internal/tui` / `internal/noninteractive` can lose subagent final text.
  - Likely fix direction for next step:
    - In `internal/agent`, when processing `EventTypeCompletedSuccess`, synthesize buffered assistant-text events from completed-turn text parts if equivalent assistant text was not already observed via completed text deltas for that turn.
  - Implemented:
    - `internal/agent` now synthesizes missing assistant-text blocks from `CompletedSuccess.Turn.Parts` while tracking completed text deltas to avoid duplicates.
    - Added focused `internal/agent` tests for completed-turn-only text in both end-turn and tool-use flows.
    - Verified with `go test ./internal/agent`.
- 2026-04-19 out-of-band review item: [P1] preserve completed-turn text order when deltas are only partial.
  - Assessment: actionable; I agree with the review item.
  - Why:
    - `missingCompletedTurnText` (`internal/agent/agent.go:594-615`) walks `Turn.Parts` against `completedTextSeen` with a single forward index, which assumes streamed completed text blocks are a prefix/subsequence of the final turn's text parts in the same order.
    - For a completed turn with text parts `["first", "second"]` and streamed completed text only for `"second"`, the helper reports only `"first"` as missing.
    - `sendOnce` then replays that missing text after `"second"` has already been buffered from the earlier `TextDelta`, so the agent-level assistant message becomes `"secondfirst"`.
  - Impact:
    - `CollectFinalAssistantText` can return corrupted text.
    - `internal/tui` and `internal/noninteractive` can display corrupted assistant text when completed-turn synthesis fills holes in the middle of a turn rather than only at the end.
  - Likely fix direction:
    - Reconcile completed-turn synthesis against the full turn structure in order, instead of treating already-streamed text as a single prefix-matched list of completed text blocks.
- 2026-04-19 out-of-band review item: [P1] replay missing text with completed-turn boundaries intact.
  - Assessment: actionable; I agree with the review item.
  - Why:
    - `sendOnce` currently loops over only the missing `TextContent` values (`internal/agent/agent.go:363-368`) and feeds them through `emitEvent` back-to-back.
    - Because `emitEvent(EventTypeAssistantText)` buffers text and there are no synthesized boundary events between those replays, text blocks that were separated by non-text parts in `CompletedSuccess.Turn.Parts` are coalesced into one assistant message.
    - Example: a completed turn shaped like `Text("draft"), Reasoning, Text("answer")` with no text deltas becomes one buffered `"draftanswer"` assistant-text event, instead of a non-final `"draft"` assistant-text message followed by the final `"answer"` message.
  - Impact:
    - The final/non-final split promised by the new agent contract is lost when assistant text must be synthesized from completed turns.
    - Downstream consumers can no longer reliably identify the true final assistant message for those turns.
  - Likely fix direction:
    - Replay synthesized completed-turn content with same-turn boundaries preserved, which likely requires tracking/reconciling more than a flat slice of completed text blocks.
- 2026-04-19: `check_spec_conformance --only_changed` passed for:
  - `internal/agent`
  - `internal/agentbuilder`
  - `internal/noninteractive`
  - `internal/tools/pkgtools`
  - `internal/tui`
- Full `review` step still pending.

## Summary

TBD

## State

- Branch: `jn/agent-message-buffering`
- Relevant packages: `internal/agent`, `internal/tui`, `internal/noninteractive`
- `internal/agent` is implemented:
  - `Event.AssistantTextFinal` added
  - assistant text is buffered per agent instance and flushed before same-agent non-text events
  - `CollectFinalAssistantText` now keys off final flagged text plus top-level `done_success`
  - package test command: `go test ./internal/agent`
- End-of-turn ordering in `internal/agent` is now `assistant_turn_complete`, then final buffered `assistant_text`, then terminal event.
- `internal/tui` is implemented:
  - descendant final-message presentation now keys off descendant `assistant_text` with `AssistantTextFinal=true`
  - `internal/tui` no longer reconstructs final descendant messages from `AssistantTurnComplete` plus buffered text
  - non-final descendant assistant text is shown literally
  - package test command: `go test ./internal/tui`
- `internal/noninteractive` is implemented:
  - descendant final-message presentation now keys off descendant final `assistant_text` events
  - `Result.FinalAssistantText` now keys off top-level final-flagged assistant text plus top-level terminal events
  - package test command: `go test ./internal/noninteractive`
- Downstream adaptation status:
  - `internal/agentbuilder` tests are updated and `go test ./internal/agentbuilder` passes
  - `internal/tools/pkgtools` tests are updated and `go test ./internal/tools/pkgtools` passes
- Changed-package SPEC conformance passed on 2026-04-19 for `internal/agent`, `internal/agentbuilder`, `internal/noninteractive`, `internal/tools/pkgtools`, and `internal/tui`.
- Review follow-up landed in `internal/agent`: completed turns now synthesize missing `assistant_text` when providers/tests do not emit completed text deltas.
- Additional `internal/agent` review follow-up is pending: completed-turn synthesis must preserve part order and same-turn text boundaries when streamed text coverage is partial.
- All planned implementation work for Phase 0 is committed; next step is the new `internal/agent` review follow-up, then re-review plus changed-package SPEC conformance for the new tree state.
- `internal/llmstream` stays provider/event-part shaped; normalization boundary remains `internal/agent`.
