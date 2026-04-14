# PR

## User Summary (do not modify)

In .prs/2026-04-12_2_review_iter.md, we added to llmstream:

```go
// A Presenter can present a tool call (and optional result) in a "semantic" way - a tree representation that can be further styled for different modalities. As
// an analogy, it's the HTML (but not the CSS) of underlying data.
type Presenter interface {
	// Present presents call and result in a semantic way (no width decisions, no assumptions about ANSI terminals, colors). To present a tool call (no result yet),
	// call Present(call, nil). To present a call with result, call Present(call, result). For instance, for a read file tool, the call might return the equivalent of
	// "Reading file.go". The result might return the equivalent of "Read file.go (123 bytes)".
	Present(call ToolCall, result *ToolResult) Presentation

	// SubagentEventPolicy defines how descendant subagent events are displayed by consumers. Tools that do not launch subagents can return SubagentEventPolicyDefault.
	SubagentEventPolicy(call ToolCall) SubagentEventPolicy
}

type SubagentEventPolicy string

const (
	SubagentEventPolicyDefault          SubagentEventPolicy = ""
	SubagentEventPolicyHideFinalMessage SubagentEventPolicy = "hide_final_message"
)
```

We used it in the the `review` tool to hide the last message (raw json), and format the result below that.

I like this UX for all current subagent-based tools (implement, change_api, clarify, update_usage. others?). So let's use `SubagentEventPolicyHideFinalMessage`

For example, in the agentbuilder spec:

```
• Investigating in path/to/pkg
  └ Find out..
    Also don't forget to...
  • (... various subagent events ...)
  • I investigated and found...
• Investigated in path/to/pkg
```

should instead change to


```
• Investigating in path/to/pkg
  └ Find out..
    Also don't forget to...
  • (... various subagent events ...)
• Investigated in path/to/pkg
  └ I investigated and found...
```
