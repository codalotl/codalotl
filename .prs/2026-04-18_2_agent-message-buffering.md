# PR

## User Summary (do not modify)

Problem:
- In internal/tui and internal/noninteractive, there's features that rely on subagent final message handling - notably, formatters for the final message (sometimes JSON, which needs to be parsed; sometimes hidden)
    - See llmstream.SubagentFinalMessagePresenter
- Determining the final message is annoying an error prone. and since there's multiple clients (tui vs noninteractive), the functionality is duplicated.
    - Example of annoying: assistant text messages come in parts. adjactent parts may (in theory) need to be combined to form one message
    - Also there can be multiple messages per turn. an assistant text, followed by a thinking blob, then another assistant text, etc.
    - This is doubly annoying because **in practice**, none of this actually happens. messages are not split. But according to the API docs' object models, it "could". And it's super rare for more than one assistant message in a turn.

In this PR:
- Move final message handling to agent
- Use in tui/noninteractive, removing the messy code from there.
- Make agent mental model: an assistant text is NOT a part - it's a full message
- agent will need to hold on to assistant text events until the next event comes in

## Design


<come up with specific rules/requirements>
<some way to flag an Event as final vs non final message>
