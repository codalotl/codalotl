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
- TUI only - ignore noninteractive (for now).
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
- We probably need to change `internal/agent` in some way, defining new events or adding fields to event.
