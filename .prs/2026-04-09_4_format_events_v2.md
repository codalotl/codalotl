# PR

## User Summary (do not modify)

Problem statement:
Today, events are formatted with `internal/agentformatter`. In particular, tool calls are formatted here. This ~works, but it has weaknesses:
- adding a new tool anywhere requires a corresponding change in agentformatter. So there's this phantom coupling.
- I want to support user-defined tools. the internal/agentbuilder and it's data/config.yml can quickly define tools. In the future, end-users can have their own tool definitions, which are basically tool calls that invoke an agent with a custom tool set and custom prompts/messages.

Similarly, "subagents" are hard-coded in internal/tui (not sure about internal/noninteractive?), and are formatted specially (to print call AND result instead of replacing call with result.)

Goal:
Tools need to own the concept of "how they're displayed".

Modify

```go
type Tool interface {
	Info() ToolInfo
	Name() string
	Run(ctx context.Context, params ToolCall) ToolResult
}
```

to include `Presenter() Presenter`.

I am not sure of the exact shape of `Presenter`, other than it's probably an interface of some kind.

Note that "how a tool call/result is displayed" is a function of:
- the actual ToolCall and ToolResult
- parameters that we currently pass to agentformatter. For instance, color schemes, plain text vs ANSI formatting, TUI vs CLI, etc.

I don't think I want to burden a Presenter interface with those parameter concerns. Instead, i think i want to invent some sort of semantic presentation, that can later be applied with color schemes, ansi formatting, width concoerns, maybe HTML in the future, etc.

PR Requirements:
- fully design the Presenter interface and "semantic presentation" idea in this PR file before any implementation.
    - Verify it works by "mentally checking" against current tools
    - Verify it indicates subagents and we can remove hard-coded lists of subagents in internal/tui
- Implement
- The user behavior should be unchanged. when we merge this PR, users should not notice a difference.
    - Exception: if we can **significantly simplify** the design in some way, we can evaluate reasonable changes to UX. For instance, maybe some tools are actually weirdly inconsistent today, some designs of Presenter make them pleasantly more consistent. Who knows?

Out of scope:
- We don't necessarily need to invent a complicated new yaml language for presenter designs in config-based tool definitions. Do something pragmatic here.
