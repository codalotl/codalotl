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
- Hide all other events inside this tool call. Like there shouldn't be a read file in between Checking and Checked.
- This should align with TUI in many ways. See how I edited tui's SPEC.md to describe how tui handles it. This situation is so simlar.
    - Big conceptual diffs: we prepend label, and we hide ~all descendant events

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
