# PR

## User Summary (do not modify)

This PR is done in the `jn/check_conformance_ui` branch (as a prerequisite for `.prs/2026-04-16_3_check_conformance_ui.md`).

Read the other PR file. In this PR file, our goal is to extend `agent` with events to support `.prs/2026-04-16_3_check_conformance_ui.md`. The change is this file is necessary, but not sufficient, to enable `.prs/2026-04-16_3_check_conformance_ui.md`.

Add event: `EventTypeStartSubagent`. `Event` will have field `StartSubagent StartSubagent`, where

```go
type StartSubagent struct {
    CallingAgentID       string // ID of agent/subagent that is creating the subagent.
    ToolCallID           string // the tool call's CallID that is creating the subagent.
    Label                string // Optional tool-supplied display label for the subagent being started
}
```

Semantics:
- Event.StartSubagent is the zero value unless Event.Type is EventTypeStartSubagent.
- EventTypeStartSubagent events are only emitted during tool calls of an already-running agent. So there's no EventTypeStartSubagent for the root agent.
- During the subagent launch process, the calling tool may optionally provide a subagent label.
- This should be optional, so existing tools do not need to change unless they want custom labeling. If omitted, StartSubagent.Label is the zero value.
- `agent` ensures exactly one EventTypeStartSubagent event happens per subagent ID.
- The event is emitted when that subagent's SendUserMessage call is accepted, not at construction time.
- EventTypeStartSubagent is the first event produced by that subagent in the shared stream.
- Creating a subagent without ever calling SendUserMessage on it does not emit EventTypeStartSubagent.
- AddUserTurn on the subagent does not emit EventTypeStartSubagent.
- For this event, AgentMeta should be the subagent's, so `evt.AgentMeta.Parent == evt.StartSubagent.CallingAgentID`.

Note:
- add this situation to agent.
- make sure nothing blows up (it will start sending these events on existing subagent-based tools)
- adapt things like tui to handle these events if necessary (they shouldn't be displayed -- mostly just dropped for now)
- Probably need to manually modify the integration tests so that they expect these events
- Retrofit one traditional subagent-based tool like clarify_public_api to supply this label and ensure that works.
- Retrofit check_spec_conformance to make sure we can ergonomically attach subagent labels in actually concurrent-based subagent tools.

In terms of the `internal/agent` package itself:
- Let's unify New vs NewWithDefaultModel into just New, which accepts options; model is one option, and subagent label is another. That's how this ultimately gets into agent.

## Plan

### Phase 0

#### Package `internal/agent` [DONE]
- Update `internal/agent/SPEC.md` for a new `EventTypeStartSubagent` event and `Event.StartSubagent`.
- Emit exactly one start-subagent event per subagent ID, only when `SendUserMessage` is accepted.
- Make that event the first shared-stream event from the subagent.
- Carry optional tool-supplied subagent labels through agent construction.
- Collapse split construction APIs into one `New(..., options)` path where `Model` is optional and `SubagentLabel` is optional.

#### Package `internal/agentregistry` [DONE]
- Update callsites that currently rely on `NewWithDefaultModel` to the unified `New(..., options)` API.
- Preserve existing default-model behavior for both root creators and subagent creators.
- No `SPEC.md` changes expected unless public docs become inaccurate during implementation.

#### Package `internal/tools/pkgtools` [DONE]
- Retrofit `clarify_public_api` to pass a useful subagent label.
- Keep existing presenter/result behavior unchanged; this is only about richer event metadata.
- Update `pkgtools`-side test fakes and any remaining local `AgentCreator` adapters to the unified `New(..., options)` API.

#### Package `internal/tools/spectools` [DONE]
- Retrofit `check_spec_conformance` to label each package-check subagent, including concurrent launches.
- Keep raw result JSON and completion semantics unchanged in this PR.

#### Package `internal/tui` [DONE]
- Ensure the new event type is tolerated and does not show up as a standalone user-visible message yet.
- Preserve existing tool-scope / subagent-policy behavior when the metadata event appears in descendant flows.

#### Package `internal/agentformatter` [DONE]
- Confirmed the formatter already keeps `EventTypeStartSubagent` invisible via its existing default-empty handling.
- No code changes were needed in this package for this PR.

#### Package `internal/noninteractive` [DONE]
- Ensure the new event type is tolerated and does not show up as a standalone user-visible message yet.
- Update focused tests where event switches or filters assume the old event set.

#### Integration / fixtures [DONE]
- Patch replay or serialized fixtures that now include `start_subagent` events from subagent-based tools.
- Keep tool-call request/response shapes stable aside from the new event-stream entries.

## Review

- Manual review (user-provided, out of band): no actionable findings. The new start-subagent metadata is emitted consistently, tolerated by existing consumers, and the updated tests for agent, TUI, noninteractive, and affected tools pass.
- `check_spec_conformance({"only_changed":true})` results:
  - conforms: `internal/agentbuilder`, `internal/agentregistry`, `internal/tools/pkgtools`, `internal/tools/spectools`, `internal/tui`
  - nonconforming: `internal/noninteractive`
    - latent minor: `Options.ReflowWidth` is specified as controlling `updatedocs` reflow width, but `opts.ReflowWidth` is not used or propagated, so setting it currently has no effect
  - package-level errors:
    - `internal/agent`: `open_ai_send_async.stream via stream error: stream ID 71; INTERNAL_ERROR; received from peer`
    - `internal/noninteractive/integration`: `open_ai_send_async.stream via stream error: stream ID 99; INTERNAL_ERROR; received from peer`
- This PR is not ready to complete yet; a follow-up step is needed to resolve or otherwise address the failed SPEC conformance state and rerun it.

## Summary

- Add `EventTypeStartSubagent` and `Event.StartSubagent` to `internal/agent`, emitting one metadata event per launched subagent when `SendUserMessage` is accepted and making it the first shared-stream event from that subagent.
- Unify agent construction onto `New(..., options)` / `AgentCreator.New(..., options)` with optional `Model` and `SubagentLabel`, while preserving existing default-model behavior for root agents and subagents.
- Retrofit labeled subagent launches in `internal/tools/pkgtools` (`clarify_public_api`) and `internal/tools/spectools` (`check_spec_conformance`) so downstream UI work can identify subagents more ergonomically.
- Keep current consumers stable by treating the new event as non-user-visible metadata in `internal/tui`, `internal/noninteractive`, and the existing formatter path.
- Update focused tests and affected integration fixtures/replay handling for subagent-based flows that now emit `start_subagent` events.

## Decisions

### Agent creation API shape
- Use one agent-construction entrypoint with optional options rather than paired `New` / `NewWithDefaultModel` methods.
- `Model` omitted means existing default behavior:
  - root creators use the package default model
  - subagent creators use the parent agent's model
- `SubagentLabel` only affects emitted `EventTypeStartSubagent` metadata.

### Consumer behavior in this PR
- `EventTypeStartSubagent` is foundational metadata for later UI work.
- Existing consumers should remain stable by ignoring or dropping the event unless they explicitly opt into using it.

## State

- This PR is a prerequisite for `.prs/2026-04-16_3_check_conformance_ui.md`.
- Relevant packages: `internal/agent`, `internal/agentregistry`, `internal/tools/pkgtools`, `internal/tools/spectools`, `internal/tui`, `internal/agentformatter`, `internal/noninteractive`.
- `internal/tools/spectools/check_spec_conformance.go` already launches concurrent subagents; this PR needs package labels there.
- `internal/tools/pkgtools/clarify_public_api.go` is a good simple single-subagent tool to retrofit for labeled launches.
- `internal/agent` now emits `EventTypeStartSubagent` once per subagent send-start and uses unified creation via `New(..., options)` / `AgentCreator.New(..., options)`.
- `internal/agentregistry` has been updated to the unified constructor API; `internal/agentbuilder` test adapters were updated as part of the same transition.
- `internal/tools/pkgtools` now wraps the per-call `AgentCreator` for `clarify_public_api` so the launched subagent gets a label derived from package + identifier, without changing tool output or presenter behavior.
- `internal/tools/spectools` now wraps each package-check subagent creator per request so concurrent `check_spec_conformance` launches get distinct labels based on the module-relative package dir.
- `internal/tui` now explicitly drops `EventTypeStartSubagent` after parent/tool-scope bookkeeping, so it does not create blank message slots; the hide-final-descendant path also treats it as metadata-only.
- `internal/noninteractive` now explicitly drops `EventTypeStartSubagent` in human-readable and JSON output paths, and its focused descendant-filter tests cover nested subagent launches.
- `internal/agentformatter` needed no code change for this PR; its existing default-empty handling already keeps `EventTypeStartSubagent` invisible.
- `internal/noninteractive/integration` updated the affected case fixtures (`pm-clarify`, `pm-clarify-stdlib`, `pm-change_api`, `pm-update_usage`) and now has test coverage around the start-subagent fixture handling.
- Validation after the `internal/agent` step:
  - passed: `go test ./internal/agent ./internal/agentregistry ./internal/agentbuilder`
- Validation after the `internal/tools/pkgtools` step:
  - passed: `go test ./internal/tools/pkgtools`
- Validation after the `internal/tools/spectools` step:
  - passed: `go test ./internal/tools/spectools`
- Validation after the `internal/tui` step:
  - passed: `go test ./internal/tui`
- Validation after the `internal/noninteractive` step:
  - passed: `go test ./internal/noninteractive/...`
- Review/conformance status:
  - manual review (user-provided) found no actionable issues
  - `check_spec_conformance({"only_changed":true})` recorded CAS for conforming changed packages
  - current blockers:
    - `internal/noninteractive` latent minor SPEC nonconformance about `Options.ReflowWidth`
    - transient package-check errors for `internal/agent` and `internal/noninteractive/integration`
