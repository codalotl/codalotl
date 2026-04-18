# PR

## User Summary (do not modify)

This is a refactoring PR that should produce no UX changes for the user.

Problem:
- Some subagents just print text as their final message
- Others print JSON
- Tools that call subagents sometimes want:
    - Just print the subagent's final textual message as-is
    - Hide the final message (usually when its JSON), and display instead on the overall tool call result
    - In the future: format the JSON and print that in subagent context (and not overall tool context).
- What we do with the final message is one choice of many in subagent-based tools. A single subagent display policy should be split into orthogonal concerns.


Task:
- llmstream: Delete SubagentEventPolicy(call ToolCall) SubagentEventPolicy and SubagentEventPolicy.
- replace with an interface upgade on the Presenter interface:

```go
type SubagentFinalMessagePresenter interface {
    // SubagentFinalMessage presents a final message of a subagent launched directly by call. This affords:
	//   - keep and display the final message as-is: return a Block-ified version of finalMessage (or better yet: just don't implement SubagentFinalMessagePresenter).
	//   - hide: return nil
	//   - format JSON: parse finalMessage + process, returning a human-readable Block.
	SubagentFinalMessage(call ToolCall, subagentLabel string, finalMessage string) Block
}
```

Notes:
- Tools don't need to implement this. If they don't: it's the same as "keep and display the final message as-is: return a Block-ified version of finalMessage".
- TUI and noninteractive or whoever can type assert a presenter to this. If the tool implements it: pass final message into this.
- Only relevant for subagents, not top-level agents.