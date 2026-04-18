# PR

## User Summary (do not modify)

This is a refactoring PR that should produce no UX changes for the user.

Problem:
- Some subagents just print text as their final message
- Others print JSON
- Tools that call subagents sometimes want:
    - Just print the subagent's final textual message as-is
    - Hide the final message (usually when it's JSON), and display it instead on the overall tool call result
    - In the future: format the JSON and print that in subagent context (and not overall tool context).
- What we do with the final message is one choice of many in subagent-based tools. A single subagent display policy should be split into orthogonal concerns.


Task:
- llmstream: Delete Presenter.SubagentEventPolicy(call ToolCall) SubagentEventPolicy and SubagentEventPolicy.
- Replace with an interface upgrade (type assert to see if presenter has extra methods) on the Presenter interface:

```go
type SubagentFinalMessagePresenter interface {
    // SubagentFinalMessage presents the final message of a subagent launched directly by call. This allows:
    //   - keep and display the final message as-is: return a Block built from finalMessage (or better yet: just don't implement SubagentFinalMessagePresenter).
    //   - hide: return nil
    //   - format JSON: parse finalMessage and transform it into a human-readable Block.
    SubagentFinalMessage(call ToolCall, subagentLabel string, finalMessage string) Block
}
```

Notes:
- Tool's presenters don't need to implement this. If they don't: it's the same as "keep and display the final message as-is: return a Block built from finalMessage".
- TUI and noninteractive or whoever can type-assert a presenter to this. If the presenter implements it: pass the final message through it.
- Only relevant for subagents, not top-level agents.
