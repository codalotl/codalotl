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
