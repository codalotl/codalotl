# PR

## User Summary (do not modify)

We just wrote the check_spec_conformance tool. It is our first tool that launches concurrent subagents. Our TUI just interleaves events inside the tool. Ex:

```
• Checking SPEC conformance
  • Read file internal/foo/SPEC.md
  • Read file internal/bar/SPEC.md
  • Read file internal/foo/foo.go
  • Read file internal/foo/foo_test.go
  • Read file internal/bar/bar.go
  • ...
• Checked SPEC conformance
  └ 1 conforming, 1 non-conforming
    internal/foo: non-conforming
      * [Latent] Y doesn't Z in the spec...
      * [New] This branch introduced issue where...
```

(you can see 2 packages above started running conformance subagents)

This is not a great UI for the end-user.

We also landed a series of refactors that aid in implementing this:
- agent events
- subagent completion
- agent final message indications
- (see last 3 .pr files before this one)

Goal:
- Display this situation and others like it better for the user.
- This description applies to the TUI.
- But noninteractive must be handled in SOME way.
    - Keeping behavior the same as today is fine.
    - It's also fine to change it some way, as long as an end-user would think it's reasonable (i'm not sure what this would be).
    - But obviously noninteractive cannot edit already-printed lines, so we can't do what the TUI does.
- Only update the end-user behavior for the check_spec_conformance tool, not other tools.
    - If we need to change interfaces/APIs/abstractions, updating other tools is allowed, but keep their end-user behavior the same for now.
    - Shared abstractions/specs may change, but resulting end-user behavior changes should stay scoped to check_spec_conformance.

Proposed UI:

The following is a **sketch** showing the **direction** I want. There are multiple manifestations of the direction. This manifestation is just an example.
- If I have a typo or something in this proposed UI, or inconsistent capitalization, or extra space, don't read into that.
- See UI Requirements below to test whether your manifestation conforms.

Mid-call of check_spec_conformance:
```
• Checking SPEC conformance
  • Subagent in internal/foo
    • Read file internal/foo/foo_test.go
  • Subagent in internal/bar
    • Read file internal/bar/bar.go
  • Subagent in internal/baz
    • Starting
```

End-call of check_spec_conformance:

```
• Checking SPEC conformance
  • Subagent in internal/foo
    • non-conforming:
      * [Latent] Y doesn't Z in the spec...
      * [New] This branch introduced issue where...
  • Subagent in internal/bar
    • Conforms
  • Subagent in internal/baz
    • Conforms
  • Subagent in internal/qux
    • Error: could not read file internal/qux/.db/x/db.var
• Checked SPEC conformance
  └ 2 conforming, 1 non-conforming, 1 error
```

UI Requirements:
- When check_spec_conformance launches a subagent, the user can see it somewhere under the "Checking SPEC conformance" tool call.
- The subagent is identified somehow.
    - Many possible options here. Ex: "• Checking SPEC conformance in internal/foo"; "• Subagent 1"
    - In this **particular** case, check_spec_conformance, i think identifying by package works, with some possible flavor text next to it (ex: "Subagent in" or "Checking conformance in" or ...). 
    - But **generally**, I want tools that launch subagents to have the **option** to identify their subagents however they want.
- Mid-subagent-run, the last event of the subagent is shown under the subagent. When new events come in, that event is replaced by the new event. When the subagent finishes, that event is replaced by the final result.
- Subagents can launch subagents. This needs to be handled in some way. There's several defensible solutions, but to keep it simple, let's just print the last event of **any** descendant in this slot. This event should be nested the same as if it were a direct subagent event.
- User can intuit which subagents are still active and which are done (for instance, in the sketch above, done subagents show a final conformance or error, and in-progress subagents have a random event like read file)
- End-subagent-run, the user can see the "result" under the subagent (which probably varies tool-by-tool).
    - For subagents that end in JSON (like this one), don't show, eg, `{"conforms":true}` to the user. User doesn't want to read JSON. It needs to be formatted somehow.
- The result shown under the final overall tool call "Checked SPEC conformance" is a summary, and doesn't show all specific nonconformances, because we showed those under the subagent.
    - This intentionally changes the current check_spec_conformance completion presentation contract, which today includes detailed nonconformance text in the final tool result body.
- In terms of concurrency, a goroutine handles "checking conformance and storing in CAS for a package". So things happen after the subagent itself is done and has produced JSON, including errors.
    - Putting these errors into the package's "slot" complicates the design.
    - Therefore, I want to NOT support doing that. These post-subagent errors will be displayed in the overall tool summary.
    - This means the user could see that a package conforms (which is true), some error occurred in saving the result. Example (exact form is NOT a requirement):

```
• Checking SPEC conformance
  • Subagent in internal/baz
    • Conforms
• Checked SPEC conformance
  └ 1 conforming
    Error when saving internal/baz conformance to file.
```

My Extra requirements of you, the Omnipotent Orchestrator:
- before you make a `## Plan`, make a `## Design`.
- Describe the actual UI you will target.
- Describe API and interface changes.
- Address anything I left open-ended for you to solve.
    - Including, but not limited to: what to do about noninteractive, how to format JSON, errors after subagent is done, etc.
- Figure out corner cases and error cases. Make sure an error at any point has a reasonable solution.
- The `## Design` section has a `### UX Tradeoffs` section. Highlight any design decisions you waffled about that impacts UX that involves a big tradeoff. Only highlight actually contentious decicions, not easy calls. This might be empty.

## Design

The recent prerequisite work already gives us almost all of the shared semantics we need:
- `.prs/2026-04-17_1_agent_events.md` added `EventTypeStartSubagent` plus tool-supplied labels. `check_spec_conformance` already labels each package-check subagent with the package dir.
- `.prs/2026-04-18_1_subagent-completion.md` replaced the old subagent-policy API with `llmstream.SubagentFinalMessagePresenter`, which is already the right hook for "this subagent ends in JSON; format it for humans".
- `.prs/2026-04-21_1_agent-m-buff-2.md` moved "finalizing assistant text" detection into `internal/agent`, so consumers no longer need to guess which descendant text is the real final answer.

Because of that, I do **not** think we need another `llmstream` presenter-interface change for this PR. The missing piece is not "how does a tool explain what a subagent is"; we already have that. The missing piece is a TUI rendering mode that can keep one mutable per-subagent slot under one long-lived tool call.

### Target TUI

I will target this concrete manifestation:

Mid-run:
```text
• Checking SPEC conformance
  • Package internal/foo
    • Read file internal/foo/foo_test.go
  • Package internal/bar
    • Read file internal/bar/bar.go
  • Package internal/baz
    • Starting
```

Completed:
```text
• Checking SPEC conformance
  • Package internal/foo
    • Non-conforming
      [Latent][major] Y doesn't Z in the spec...
      [New][minor] This branch introduced issue where...
  • Package internal/bar
    • Conforms
  • Package internal/baz
    • Conforms
  • Package internal/qux
    • Error: could not read file internal/qux/.db/x/db.var
• Checked SPEC conformance
  └ 2 conforming, 1 non-conforming, 1 error
    CAS write error for internal/baz: permission denied
```

Important behaviors:
- The outer `"Checking SPEC conformance"` call message stays visible for the entire run.
- Each direct package-check subagent gets one stable slot under that call.
- While the package check is running, the slot shows exactly one live child block: the latest event from that package's descendant tree.
- When the direct package-check subagent finishes, that live child block is replaced by the package result.
- Nested subagents do **not** get their own separate slots in this PR. Their events feed the nearest package slot.
- The final `"Checked SPEC conformance"` message is a compact summary in TUI. It does not repeat every nonconformance, because that detail already lives under the package slots.

### Event And Rendering Model

This stays tool-specific in end-user behavior, but it should be implemented by leaning on existing generic event metadata instead of inventing new generic APIs.

For a `check_spec_conformance` tool call, TUI will track a per-call display state:
- keyed by the parent tool call ID
- storing the original tool-call message index
- storing one slot per direct subagent agent ID

Each slot stores:
- display label
- direct subagent agent ID
- current rendered state
- whether the direct subagent has reached a terminal slot state yet

Slot lifecycle:
1. On `EventTypeStartSubagent` for a descendant whose enclosing tool scope is `check_spec_conformance`, create a slot if one does not already exist.
2. Initial slot state is `Starting`.
3. On any later descendant event in that package subtree, update the slot's live state to that event, replacing the prior live state.
4. On the direct package subagent's finalizing assistant text, replace the live state with the formatted package result and mark the slot done.
5. On the direct package subagent's `error` or `canceled`, replace the live state with that terminal event and mark the slot done.
6. On direct package subagent `done_success` without a finalizing assistant text having been seen, mark the slot done with a fallback state like `No final result`. The overall tool summary will still carry the package-scoped error produced by `check_spec_conformance`.

Nested subagents:
- Keep one slot per direct package-check subagent.
- To find which slot owns an arbitrary descendant event, walk ancestry from the event's agent up to the tool-calling agent and pick the first child under that tool scope.
- This means a nested subagent's `read_file` event temporarily becomes the package slot's live preview, which is exactly the simplification requested in the user prompt.

Rendering details:
- Live descendant events should reuse the normal formatter, but with slot-local indentation rather than true agent-depth indentation. In practice, TUI can render the event as if `Agent.Depth == 0` and then place that text under the slot body.
- Direct package final messages should go through the existing `llmstream.SubagentFinalMessagePresenter` hook, not raw JSON printing.
- `EventTypeStartSubagent`, `EventTypeAssistantTurnComplete`, and `EventTypeDoneSuccess` are metadata for the slot tracker, not visible slot content on their own.

### `spectools` Result Contract

There is one real contract gap in the current implementation: `packageCheckResult` cannot currently represent "the subagent concluded that the package conforms, but a later per-package side effect failed". Today that later failure overwrites the conforming result with `error`, which makes the final UI impossible to represent faithfully.

I think the result shape should change to distinguish:
- package verdict failure: no valid package result was produced
- post-verdict per-package failure: a valid verdict was produced, but later work for that package failed

Concretely, extend each package object with a second error channel:

```json
{
    "internal/baz": {
        "conforms": true,
        "postcheck_error": "store CAS conformance: permission denied"
    }
}
```

Rules:
- `error` remains mutually exclusive with `conforms` / `nonconformances`. It means the package check failed before producing a valid verdict.
- `postcheck_error` may coexist with a valid verdict. It means the verdict is still true and should still drive the package slot, but the overall tool summary should surface the follow-on failure.
- Invalid or schema-bad subagent JSON remains `error`, not `postcheck_error`.
- Raw `ToolResult.Result` stays machine-readable JSON.

This is the only shared data-model change I think is actually required for the UX the user asked for.

### `spectools` Presentation Changes

`check_spec_conformance` should stop returning `nil` from `SubagentFinalMessage`.

Instead, `SubagentFinalMessage(call, subagentLabel, finalMessage)` should:
- parse the final JSON
- return a human-readable block for the package slot
- never show raw JSON to the end user

Formatting policy:
- `{"conforms":true}` => `Conforms`
- `{"conforms":false,...}` => `Non-conforming` plus one line per nonconformance
- invalid JSON or an invalid JSON shape => concise fallback text such as `Invalid conformance result`

That fallback is still useful even though the overall tool result will also contain a package-scoped error. It avoids showing raw JSON or an empty slot when the subagent did emit something, but emitted the wrong thing.

For the overall tool completion body, there are two audiences:
- TUI: wants a summary-only body because details moved into the package slots
- noninteractive: still benefits from the current detailed end-of-tool body because it cannot rewrite already-printed output

So I would **not** make the presenter itself TUI-shaped. Instead:
- keep shared parsing/helpers in `internal/tools/spectools`
- let TUI render its own compact completion body for this one tool
- let noninteractive keep a fuller completion body

That keeps the scope aligned with "change end-user behavior only for `check_spec_conformance` in TUI".

### TUI Package Changes

The TUI needs a small tool-specific rendering layer, not a new global presenter protocol.

Add TUI-internal state for active `check_spec_conformance` calls:
- associate the tool call with the message index for the original `"Checking SPEC conformance"` message
- keep stable slot order in first-seen order from `StartSubagent`
- invalidate and re-render that one message whenever a slot changes

The `"Checking SPEC conformance"` message becomes a composite render in TUI:
- normal tool-call header from the existing formatter
- plus the slot list underneath it

The `"Checked SPEC conformance"` message also becomes a composite render in TUI:
- normal tool-complete header
- plus a compact summary block derived from the raw tool result JSON
- plus any `postcheck_error` lines

This should stay local to TUI. Other tools keep using the existing append/replace message behavior.

I do not think `internal/agentformatter` needs a new public formatting abstraction for this PR. TUI can:
- reuse the normal event formatter for live slot previews
- reuse `agentformatter.RenderPlainTextBlock` for presenter-produced final slot blocks
- build the slot list / compact summary in plain TUI-owned text

### Noninteractive

Noninteractive does not have a good way to edit prior lines, so it should not try to mimic the slot UI.

The reasonable behavior here is:
- keep current streaming behavior during the run
- keep printing descendant events in the order they happen
- keep the final overall tool result as the place where full package details can be read
- update that final overall output only as needed to account for `postcheck_error`

Concretely:
- human-readable noninteractive output stays essentially as it works today
- JSON output continues to emit the raw event stream plus the raw tool result JSON
- integration fixtures will need intentional updates if the result JSON shape changes

This gives TUI the improved UX without making noninteractive worse.

### Shared Helper Shape

To avoid duplicating result parsing and result-to-display logic, `internal/tools/spectools` should own helpers that both the presenter and TUI can call.

Likely helper responsibilities:
- parse raw tool result JSON into a typed summary structure
- compute counts for conforming / non-conforming / hard-error packages
- collect `postcheck_error` lines
- format one package subagent final JSON payload into a human-readable block

That is a code-sharing change, not a new user-visible abstraction.

### Corner Cases

- Slot label missing:
  - Fallback to `Package <n>` or `Subagent <n>` rather than rendering a blank header.
- Direct subagent emits no visible events before finishing:
  - Slot still appears from `StartSubagent` with `Starting`, then moves to final result or fallback terminal text.
- Direct subagent final text is invalid JSON:
  - Slot shows a concise invalid-result placeholder.
  - Overall tool result records a package `error`.
- Direct subagent succeeds, then CAS write fails:
  - Slot remains `Conforms` or `Non-conforming`.
  - Final overall summary shows the `postcheck_error`.
- Nested subagent activity after the direct subagent already finalized:
  - Treat as impossible/ignored for slot rendering; the direct subagent final state wins.
- Multiple concurrent package checks:
  - Slot order is first-seen order and does not reshuffle later.
- Empty overall result `{}`:
  - TUI still shows `Checked SPEC conformance` with `No eligible packages.`

### UX Tradeoffs

- TUI-only compact completion vs changing the shared presenter for everyone:
  - I think TUI-only is the right call.
  - If we changed the shared presenter to summary-only, noninteractive would lose the one place where it can still show full nonconformance detail. That would be a UX regression outside the target scope.

- Showing package verdict in the slot even when later CAS persistence fails:
  - This creates a slightly split presentation: the package slot can say `Conforms` while the final summary also shows an error for that same package.
  - I still think this is the right behavior because it reflects the actual lifecycle boundary the user explicitly called out: the package verdict was real; the later side effect failed.

## Plan

### Phase 0

Land the shared result/presentation changes first, then teach TUI to render `check_spec_conformance` as a composite tool call with stable package slots.

#### Package internal/tools/spectools [DONE]
- Update `internal/tools/spectools/SPEC.md` for:
  - `postcheck_error` on otherwise-valid package results
  - human-readable package slot formatting via `SubagentFinalMessage`
  - split presentation between TUI compact summary and fuller noninteractive completion body
- Add shared parsing/formatting helpers for package results and summary counts.
- Keep raw tool result machine-readable JSON.

#### Package internal/tui [DONE]
- Update `internal/tui/SPEC.md` for tool-specific package-slot rendering for `check_spec_conformance`.
- Add per-tool-call state for direct package subagents.
- Render one stable slot per direct package subagent label.
- Show latest descendant event while active, then replace with terminal package result.
- Render compact completion summary plus `postcheck_error` lines under `Checked SPEC conformance`.

#### Validation
- Run focused tests for `internal/tools/spectools` and `internal/tui`.
- Defer `review` and `check_spec_conformance({"only_changed":true})` until after initial implementation is complete.

## Review

Pending.

## Summary

Pending.

## State

- Branch: `jn/spec-conform-ui-v3`
- Current implementation targets two packages:
  - `internal/tools/spectools` for result contract + package/result formatting helpers
  - `internal/tui` for tool-specific slot rendering
- Existing prerequisites already present:
  - start-subagent events with labels
  - finalizing assistant text detection in `internal/agent`
  - `llmstream.SubagentFinalMessagePresenter`
- `internal/tools/spectools` landed:
  - `postcheck_error` preserves valid package verdicts when CAS writes fail
  - shared parsing/summary/package-formatting helpers exist for TUI reuse
  - presenter now formats package final JSON into human-readable blocks
- Focused validation done: `go test ./internal/tools/spectools`
- `internal/tui` landed most of the requested UX:
  - stable package slots under the active call
  - nested descendant events collapse into the owning package slot
  - compact completion summary uses shared `spectools` helpers
- `internal/tui` follow-up landed:
  - overall tool-error completions keep the normal error rendering path
- Validation done:
  - `go test ./internal/tui`
  - `go test ./...`
