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

## Plan

### [DONE] Phase 0

#### [DONE] Package internal/agentbuilder
- Update `subagent_q_and_a` presenter design to hide descendant final assistant messages and surface tool result text on the outer completion when configured.
- Update built-in `implement` tool config to show the subagent result in the outer completion body instead of relying on the nested final message.
- Update YAML presenter tests for completion body + `SubagentEventPolicy`.

#### [DONE] Package internal/tools/pkgtools
- `clarify_public_api`, `change_api`, and `update_usage` are the current hand-written subagent-backed tools in this package.
- Use `SubagentEventPolicyHideFinalMessage` for those presenters.
- Keep `clarify_public_api` completion body behavior.
- Add outer completion bodies for `change_api` and `update_usage` so the subagent's final text still appears after the nested final message is hidden.
- Update presenter tests accordingly.

#### [DONE] Validation
- Ran focused tests for `internal/agentbuilder`.
- Ran focused tests for `internal/tools/pkgtools`.
- Updated `internal/noninteractive/integration` expectations for changed pkgtools subagent event output (`pm-change_api`, `pm-clarify`, `pm-clarify-stdlib`, `pm-dependency`, `pm-update_usage`).
- Ran focused tests for `internal/noninteractive/integration`.
- Ran `go test ./...`.

## Review [DONE]

- Review against `main` found two actionable P2 issues in `internal/tools/pkgtools`.
- `pkgToolResultPayloadContent` currently treats any valid JSON object as the internal `{content,error}` envelope. If a successful subagent reply is raw JSON without those keys, `change_api` / `update_usage` now hide the nested final assistant message and also drop the outer completion body.
- `clarifyPublicAPIPresenterResultContent` has the same issue, so a successful raw-JSON clarification answer can disappear entirely once nested final assistant messages are hidden.
- [DONE] Actioned in `c363165`: pkgtools presenters now only treat the explicit envelope shape specially, preserving arbitrary raw JSON text in `change_api`, `update_usage`, and `clarify_public_api`. Added focused regression tests and re-ran `go test ./internal/tools/pkgtools`.
## Summary

## State

- Branch: `jn/use-subagent-policy`
- Existing policy support already lands in `llmstream`, `tui`, `noninteractive`, and review presenters.
- Current subagent-backed presenters in scope: YAML `implement` via `subagent_q_and_a`, plus pkgtools `clarify_public_api`, `change_api`, and `update_usage`.
- I did not find other current tool presenters in repo that both launch subagents and still return `SubagentEventPolicyDefault`.
- `internal/agentbuilder` is implemented: `subagent_q_and_a` now hides nested final messages, and built-in `implement` now shows the subagent result in the outer completion body.
- `internal/tools/pkgtools` is implemented: `clarify_public_api`, `change_api`, and `update_usage` now hide nested final messages; `change_api` and `update_usage` now surface result text on the outer completion body.
- Review feedback is fixed: raw JSON subagent answers are preserved as visible output unless the result is the explicit pkgtools envelope.
- `internal/noninteractive/integration` expectations are updated for the new presented event stream; full `go test ./...` now passes.
