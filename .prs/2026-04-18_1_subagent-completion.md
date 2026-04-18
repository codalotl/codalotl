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

More instructions to orchestrator:
- When you call implement, instruct implement to pass along pr context with @mentions whenever it uses tools like `update_usage` or `change_api`.

## Plan

### Package `internal/llmstream` [DONE]
- Replace `Presenter.SubagentEventPolicy` and `SubagentEventPolicy` with optional `SubagentFinalMessagePresenter`.
- Default behavior when a presenter does not implement the extra interface: keep displaying descendant subagent final text as-is.
- `SubagentFinalMessage` applies only to subagents launched by a tool call; top-level assistant messages stay unchanged.
- Update `internal/llmstream/SPEC.md` and package tests to reflect the new interface.

### Package `internal/tui`
- Replace stored descendant subagent policy state with descendant final-message presentation state.
- When a tool presenter implements `llmstream.SubagentFinalMessagePresenter`, use its returned `Block` for descendant subagent final messages.
- When `SubagentFinalMessage` returns `nil`, suppress the descendant subagent final message.
- When the tool presenter does not implement the interface, keep current plain-text display behavior.
- Update `internal/tui/SPEC.md` and focused tests.

### Package `internal/noninteractive`
- Mirror the same descendant subagent final-message behavior in human-readable output and JSON mode.
- Preserve existing top-level output behavior and machine-readable tool results.
- Update `internal/noninteractive/SPEC.md` and focused tests.

### Package `internal/agentbuilder`
- YAML presenter presets that previously hid descendant subagent final messages should implement `llmstream.SubagentFinalMessagePresenter` and return `nil`.
- Keep the existing call/result presentations unchanged.
- Update `internal/agentbuilder/SPEC.md` and presenter tests.

### Package `internal/tools/pkgtools`, `internal/tools/spectools`
- Subagent-launching presenters that previously hid descendant subagent final messages should implement `llmstream.SubagentFinalMessagePresenter` and return `nil`.
- Presenters that only relied on the old default policy should stop carrying policy methods.
- Update `internal/tools/pkgtools/SPEC.md` and relevant tests.

### Package `internal/tools/coretools`, `internal/tools/exttools`, `internal/agentformatter`
- Remove now-unneeded default `SubagentEventPolicy` methods and tests that only asserted the default behavior.

### Validation
- Current package validation: `go test ./internal/llmstream`
- Run focused `go test` for the touched packages.
- Watch for noninteractive integration expectations that depend on descendant subagent final-message printing; this refactor should preserve current UX.

## Review

Not run yet.

## Summary

Pending.

## State

- Branch: `jn/subagent-final-message`
- PR goal: refactor descendant subagent final-message presentation without changing visible UX.
- `internal/llmstream` now exposes optional `SubagentFinalMessagePresenter`; `Presenter.SubagentEventPolicy` and `SubagentEventPolicy` are removed.
- Downstream packages still to update for the new API: `internal/tui`, `internal/noninteractive`, `internal/agentbuilder`, `internal/tools/pkgtools`, `internal/tools/spectools`, `internal/tools/coretools`, `internal/tools/exttools`, `internal/agentformatter`.
- `internal/agentbuilder` and `internal/tools/pkgtools` explicitly depend on hiding descendant subagent final messages today.
