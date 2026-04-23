# PR

## User Summary (do not modify)

We just landed .prs/2026-04-21_2_check_conformance_ui_v3.md, which works well for the TUI.

The noninteractive mode, though, is less good. Events are interleaved. Worse, it's not clear which package the final conformance/nonconformance messeage belongs to.

In this PR, tidy up noninteractive for checking spec conformance.

- Try not to change other packages (tui, spectools, etc).
- Try not to introduce more abstractions/interfaces/etc.
- Try to locally improve just noninteractive somehow, as it relates to check_spec_conformance.
- Do not do hacks.
- Do not hard-code check_spec_conformance in particular into noninteractive. Anything we do must generalize. See TUI for how we handled it.
- Keep JSON mode unchanged.

Specifics of solution:

Add a generic "labeled subagent lifecycle" that activates only when:
- the subagent has a non-empty StartSubagent.Label

Then print:

- One line when the subagent starts
- One label-prefixed "entry" when the subagent finishes:
    - either the ending message,
    - or the presented ending message,
    - or something similar to "X finished" where X is the label (this path is only if the presenter returns nil)
- (These will necessarily be interleaved in some way)

Example output (this shows overall shape, not exact requirements. I could have typos or oversights):

```
> .prs/2026-04-22_1_parallel-noninteractive.md - do not do steps. jsut run check_spec_Conformance(True) and end your turn without commits or pr file edits
• Using spec-md because you asked for a SPEC conformance check only.
• Read /home/jonathan/.codalotl/skills/.system/spec-md/SKILL.md
• Checking SPEC conformance
  • internal/foo: started
  • internal/bar: started
  • internal/bar: does not conform
    [new][minor] bar does not...
  • internal/foo: conforms
• Checked SPEC conformance
  └ 1 conforming, 1 non-conforming, 0 errors
• Finalizing response
• Ok I did it, the results were...
```

Validation:
- As part of the implementation plan, you must manually test this.
- Run something like `go run . exec --yes --slash-command="orchestrate" ".prs/2026-04-22_1_parallel-noninteractive.md - do not do steps. just run check_spec_conformance(true) and end your turn without commits or pr file edits"`
- Make sure everything looks good. If not, iterate.
- NOTE: running the above has side effects (CAS writes on conformance). you'll have to deal with that.

## Plan

### Package internal/noninteractive
- Keep this change local to `internal/noninteractive`.
- Keep JSON mode unchanged.
- Add a human-readable labeled subagent lifecycle path that is its own display path, not ordinary descendant assistant text.
- The lifecycle activates only when `EventTypeStartSubagent` has a non-empty label.
- Print one start entry for the labeled subagent.
- Print one completion entry for the labeled subagent on every terminal path:
  - presented ending message
  - finalizing assistant text
  - `done_success`
  - `error`
  - `canceled`
- Make the completion entry clearly belong to the label/package, even when multiple labeled subagents interleave.
- Preserve existing unlabeled subagent behavior.
- Preserve existing delayed tool-call behavior unless it directly conflicts with readable labeled lifecycle output.
- Add focused tests in `internal/noninteractive` that exercise the actual human-readable shape, especially parallel/interleaved labeled runs.

### Spec / output shape
- Update `internal/noninteractive/SPEC.md` to reflect the lifecycle behavior once the exact human-readable shape is chosen.
- Validate the shape against real CLI output, not just formatter-unit expectations.

## Learnings

- Do not implement lifecycle entries by synthesizing `assistant_text` events. That makes the lifecycle render as ordinary descendant chat/prose, which is noisy and looks wrong in real CLI output.
- Matching the request mechanically is not enough; this needs to follow the TUI's spirit of giving labeled subagents distinct display ownership/status, not pretending they are assistant messages.
- Completion handling must cover all terminal paths, not just finalizing assistant text and `done_success`.
- Real `go run . exec ...` output is the UX bar for this PR. A change that looks acceptable in narrow tests can still be obviously wrong in the real stream.
- This PR needs at least one test or manual validation scenario with multiple interleaved labeled subagents; otherwise we can regress into unreadable output again.

## Review

Pending.

## Summary

Pending.

## State

- Target package remains `internal/noninteractive`.
- Current code suppresses `EventTypeStartSubagent` in human-readable mode.
- Previous attempt was removed from history at user request.
- Main failed idea: routing lifecycle through assistant-text rendering instead of a distinct human-readable lifecycle entry.
