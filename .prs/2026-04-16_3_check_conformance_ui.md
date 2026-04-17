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

Goal:
- Display this situation and others like it better for the user.
- TUI only - ignore noninteractive for now, keeping behavior ~as-is.
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
- User can intuit which subagents are still active and which are done (for instance, in the sketch above, done subagents show a final conformance or error, and in-progress subagents have a random event like read file)
- End-subagent-run, the user can see the "result" under the subagent (which probably varies tool-by-tool).
- The result shown under the final overall tool call "Checked SPEC conformance" is a summary, and doesn't show all specific nonconformances, because we showed those under the subagent.
    - This intentionally changes the current check_spec_conformance completion presentation contract, which today includes detailed nonconformance text in the final tool result body.

Other notes:
- We probably want to use SubagentEventPolicy, defining a new policy.

## Plan

### Phase 0

#### Package `internal/llmstream` [DONE]
- Add a new `SubagentEventPolicy` variant for tools that want descendant activity summarized by subagent instead of interleaved as normal messages.
- Policy must let tools identify subagents however they want; for this workflow, the TUI will use `StartSubagent.Label`.
- Keep existing policies and non-subagent tools unchanged.

#### Package `internal/tui` [DONE]
- Implement policy-aware subagent summary rows under the parent tool call.
- When a summarized subagent starts, show a labeled row under the parent tool.
- While that subagent is active, replace its child detail with the latest visible descendant event.
- When that subagent finishes, replace the child detail with its final result or error so completed vs active work is easy to read.
- Do not print raw `start_subagent` events as standalone messages.

#### Package `internal/tools/spectools` [DONE]
- Update `check_spec_conformance` to opt into the new summarized-subagent policy.
- Keep per-package identification aligned with the existing subagent label, which is already the module-relative package dir.
- Change the final completed tool body to a summary only; detailed nonconformances and package errors should live under the per-package subagent rows.
- Keep raw `ToolResult.Result` JSON unchanged.

#### Validation [DONE]
- Update/add tests for TUI subagent summarization, including in-progress replacement, final per-subagent result rendering, and unchanged behavior for existing policies.
- Update/add presenter tests for the `check_spec_conformance` completion summary.
- Ran:
  - `go test ./internal/llmstream`
  - `go test ./internal/tui`
  - `go test ./internal/tools/spectools`

#### Review follow-up
- `internal/tools/spectools`: keep `check_spec_conformance` on a noninteractive-supported subagent policy so human/json sessions do not regress into noisy interleaving.
- `internal/tools/spectools`: keep package-specific error detail in the completed tool body for failures that happen before or after the package subagent run, since those do not currently have a guaranteed per-package summarized row.
- `internal/tui`: do not permanently mark a summarized package row as done based only on descendant agent completion when outer-package postprocessing can still fail.
- Re-run review and `check_spec_conformance({"only_changed": true})` after the fixes.

## Review

Review run against `main` found three actionable issues:
- `internal/tools/spectools/check_spec_conformance.go`: `SubagentEventPolicySummarizeBySubagent` is only implemented by the TUI path today, so noninteractive sessions regress to interleaved descendant events unless `check_spec_conformance` stays on a policy they already understand.
- `internal/tools/spectools/check_spec_conformance.go`: package failures that happen before launching a subagent, or after a subagent returns, are now hidden from the final completion body even though they may never appear under a summarized package row.
- `internal/tui/tui.go`: summarized rows can flip to `[done]` on descendant-agent success before outer-package parsing/CAS persistence finishes, so the row can disagree with the real package result.

`check_spec_conformance({"only_changed": true})` result:
- `internal/llmstream`: conforms
- `internal/tui`: conforms
- `internal/tools/spectools`: non-conforming
  - major/new: finished package rows are required to show each package's final result, including package-scoped errors; the current implementation only changes the subagent policy and aggregate completion summary, so prep/parsing/CAS-write failures are not guaranteed to surface in the per-package presentation.

## Summary

Pending.

## Decisions

- Use a new `SubagentEventPolicy` rather than special-casing `check_spec_conformance` in the TUI.
- Use `StartSubagent.Label` as the tool-controlled subagent identifier. `check_spec_conformance` already labels subagents with the package path, so no new tool-result schema is needed.

## State

- Branch: `jn/check_spec_conformance_ui`
- `internal/tools/spectools/check_spec_conformance.go` already launches one labeled subagent per eligible package via `labeledSubAgentCreator`.
- Landed in `d7e3b98`: new `SubagentEventPolicySummarizeBySubagent`, TUI summarized subagent rows keyed by label, and `check_spec_conformance` summary-only completion body.
- `internal/tui/tui.go` now tracks per-tool summarized subagent rows in the tool display scope and updates each row from descendant events.
- `check_spec_conformance` now opts into the new policy; package labels still come from the existing package-key subagent label.
- Review + conformance step found real gaps around noninteractive behavior and package-level failures that occur outside the descendant subagent happy path.
- CAS files were written for changed conforming packages during the latest conformance run.
