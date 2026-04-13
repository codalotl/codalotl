# PR

## User Summary (do not modify)

### Background / Problem:

During a recent PR, redid formatters for all tools as presenters (See .prs/2026-04-11_1_format_events_v3.md). For the review tool, we instruct the agent to have its last message be JSON. The problem is that the user sees a wall of JSON s the last message from the subagent. The review tool then correctly prints the tool result.

In an ideal world, the user just wouldn't see this final message. This isn't a bug in the review tool presenter itself per se. Rather, a limitation of the system.

Related problem: in the future, I will want to make parallel subagents. I will need to change the TUI UI for this, since I don't think ~randomly interleaving agent messages is useful. In my head, the future UI will look like:

* Refactoring 7 packages (internal/foo, internal/bar, ...)
  * Refactoring internal/foo
    * Read File internal/foo/file.go
  * Refactoring internal/foo, internal/bar
    * Thinking about next step
  * Refactoring internal/baz
    * Edit internal/baz/core.go
      └ + if isTest {
        - if isTest && otherondition {

In other words, only show the last event per subagent, which gets replaced as a new event comes in for that subagent.

At the same time, this task isn't about agent event suppression in general. We may want multiple consumers of the agent event stream: the UI, loggers, debuggers, etc. So I believe all events must happen. This is a UI concern: TUI and noninteractive just need to choose how to display the events.

### Requirements of this PR

* Concretely, I want to fix the review tool displaying that blob of JSON to the end-user. It should not be displayed in TUI, nor noninteractive.


### Sketch of design

Edit internal/llmstream. Add to `Presenter` interface. No, don't make a separate interface.

```go
// A Presenter enables tools to define UI formats and policies for how the tool call/result are displayed, as well as how to subagent event streams that come "from" a tool call.
//   - It can present a tool call (and optional result) in a "semantic" way - a tree representation that can be further styled for different modalities. As
//     an analogy, it's the HTML (but not the CSS) of underlying data.
//   - When a tool call launches a subagent, the tool can define policies for how that subagent's events are displayed. For instance, we may want to display all subagent events (the default),
//     or selectively hide some events (i.e., the last message).
type Presenter interface {
	// Present presents call and result in a semantic way (no width decisions, no assumptions about ANSI terminals, colors). To present a tool call (no result yet),
	// call Present(call, nil). To present a call with result, call Present(call, result). For instance, for a read file tool, the call might return the equivalent of
	// "Reading file.go". The result might return the equivalent of "Read file.go (123 bytes)".
	Present(call ToolCall, result *ToolResult) Presentation

  // SubagentEventPolicy defines how events are displayed. Only relevant for tools that launch subagents (all other tools can safely return SubagentEventPolicyDefault). The
  // argument `call` is the tool call that launched the subagent (not a call made by the subagent) - it lets the policy be a function of the input parameters.
  SubagentEventPolicy(call ToolCall) SubagentEventPolicy
}

type SubagentEventPolicy string

const (
    SubagentEventPolicyDefault          SubagentEventPolicy = ""
  	SubagentEventPolicyHideFinalMessage SubagentEventPolicy = "hide_final_message"
)
```

NOTE: this design enables `SubagentEventPolicyHideAll          SubagentEventPolicy = "hide_all"` and `SubagentEventPolicyLast             SubagentEventPolicy = "last"`, but those policies are out of scope here.

Based on that:
- make review tool return SubagentEventPolicyHideFinalMessage
- Update TUI to handle
- Update noninteractive to handle
