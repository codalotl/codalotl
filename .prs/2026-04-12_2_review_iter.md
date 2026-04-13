# PR

## User Summary (do not modify)

During a recent PR, redid formatters for all tools as presenters (See .prs/2026-04-11_1_format_events_v3.md). For the review tool, we instruct the agent to have its last message be JSON. The problem is that the user sees a wall of JSON s the last message from the subagent. The review tool then correctly prints the tool result.

In an ideal world, the user just wouldn't see this final message. This isn't a bug in the review tool presenter itself per se. Rather, a limitation of the system.

Task:

- Identify 3 possible solutions here:
    - First: the ideal solution. Something that doesn't break abstraction boundaries. Something clean that gives subagents a clean way to limit messages somehow?
    - Second: a pragmatic solution to quickly solve this.
    - Third: something else

Once you produce this artifact in this PR file (NOTE: they MUST be detailed and concrete enough to be implementable), I will pick one for you to implement.

## Plan

### [DONE] Option 1 - first-class subagent visibility policy (ideal)

- Goal:
  - Keep the current subagent abstraction, but let the caller declare which parts of a subagent's output are mirrored into the parent event stream versus kept private for result collection.
  - Use that policy for machine-readable tools like `review`, so the JSON-producing final assistant message is still available to the tool implementation but is never surfaced to the user.
- Main package changes:
  - `internal/agent`
    - Extend subagent creation so a parent can choose a mirror policy when creating a child agent.
    - The minimum useful policy surface is:
      - `mirror_all` (current behavior)
      - `hide_final_assistant_text` (mirror normal child activity, but do not forward the child agent's terminal `assistant_text` / final completed turn text into the parent-visible stream)
    - The cleanest shape is a `SubAgentOptions`/`MirrorPolicy` value owned by `agent`, because that is the layer that currently mirrors child events in `dispatchEvent` / `relayFromChild`.
  - `internal/tools/toolsetinterface`
    - Add a field on `InvokeRequest` that lets a tool request the subagent mirror policy when invoking another agent.
    - Keep the default equal to today's behavior so existing tools are unchanged.
  - `internal/agentregistry`
    - Thread the new `InvokeRequest` field through `Prepare` / `Create` / `Invoke` into subagent creation.
  - `internal/agentbuilder`
    - Extend YAML `subagent` config with a field such as `event_visibility: hide_final_assistant_text`.
    - Apply it to the built-in `review` tool in `internal/agentbuilder/data/config.yml`.
    - Optionally apply it to other machine-readable subagent tools later if they have the same problem.
  - `internal/tui` and `internal/noninteractive`
    - No special casing for `review`.
    - They just render the parent-visible event stream; the hidden final JSON event never arrives there.
- Behavioral details:
  - Child tool calls, retries, warnings, and intermediate reasoning/text can still be mirrored if desired.
  - The tool implementation still reads the child agent's dedicated event channel and still uses `agent.CollectFinalAssistantText`, so `result_format: json` continues to work without changing the presenter contract.
  - Token usage should continue to aggregate into the parent, because the child is still a real subagent.
- Test plan:
  - `internal/agent`: add focused tests proving that a child created with the new policy still mirrors normal tool events but suppresses the terminal assistant text from the parent stream while preserving it on the child's own stream.
  - `internal/agentbuilder` / `internal/agentregistry`: add tests that the YAML field is parsed, passed through, and applied by the built-in `review` tool.
  - `internal/tui` and `internal/noninteractive`: add regression coverage showing a `review` run no longer prints the raw JSON wall, while the final presenter-owned `Reviewed origin/main` output still appears.
- Tradeoffs:
  - Best abstraction and reusable for future machine-readable subagents.
  - Touches multiple packages and lightly expands the subagent API surface.

### [DONE] Option 2 - UI-level suppression for `review` only (pragmatic / fastest)

- Goal:
  - Fix the user-visible problem quickly without changing subagent creation semantics.
  - Teach visible renderers to drop the noisy descendant assistant message for the `review` tool while leaving the underlying agent system untouched.
- Main package changes:
  - `internal/tui`
    - Track when a top-level `review` tool call is active.
    - While that tool call is active, suppress descendant `EventTypeAssistantText` and descendant `EventTypeAssistantTurnComplete` display events that belong to the review subagent.
    - Continue showing descendant tool activity (for example the `git log` / `git diff` shell calls), so the user still sees that review work is happening.
  - `internal/noninteractive`
    - Mirror the same suppression logic for human-readable output so CLI/TUI stay consistent.
    - Leave JSON event output untouched unless we explicitly want parity there too.
- Concrete implementation shape:
  - In both renderers, maintain lightweight state keyed by the active top-level tool call ID for `review`.
  - When a `review` tool call starts, mark descendant assistant-text events as hidden until the matching `ToolComplete`.
  - Only suppress child assistant text for that tool; do not suppress root-agent text or descendant tool events.
- Why this is fast:
  - No changes to `internal/agent`, `internal/agentregistry`, or YAML subagent semantics.
  - No changes to `review` prompt/result parsing.
  - Very small blast radius.
- Test plan:
  - `internal/tui`: add a regression test that reproduces a `review` call with descendant JSON assistant text and verifies that only the presenter-owned review result is visible.
  - `internal/noninteractive`: add the same regression for formatted CLI output.
- Tradeoffs:
  - This breaks abstraction boundaries by teaching UI code about a specific tool's child-event noise pattern.
  - If another tool later has the same problem, we either duplicate the hack or come back and implement Option 1.
  - The raw event stream still contains the JSON; only visible rendering changes.

### [DONE] Option 3 - detach machine-readable review runs from the parent stream

- Goal:
  - Keep the current user-facing behavior clean by not running `review` as a mirrored subagent at all.
  - The review tool still uses an agent internally, but that agent is invoked on a private event channel that the tool consumes directly.
- Main package changes:
  - `internal/agentbuilder`
    - Add a per-subagent execution mode for YAML subagent tools, for example `execution_mode: detached`.
    - When detached, do not pass through the inherited `SubAgentCreatorFromContext`; instead invoke the target agent with a fresh root `agent.NewAgentCreator()`.
    - Keep `result_format: json` and the existing presenter behavior exactly the same.
  - `internal/agentbuilder/data/config.yml`
    - Apply detached execution to `review` only.
  - `internal/agentbuilder` tests
    - Add coverage that detached subagent execution returns parsed JSON correctly and does not emit descendant child events into the parent-visible stream.
- Behavioral details:
  - The review agent still runs with the same prompt/messages and still returns the same JSON payload to the outer tool.
  - Because the child is no longer a true subagent, the parent UI sees only:
    - `Reviewing origin/main`
    - `Reviewed origin/main`
    - the presenter-owned human-readable findings
- Tradeoffs to call out explicitly:
  - This is implementable with a much smaller surface than Option 1.
  - It is cleaner than a TUI hack, but it changes semantics:
    - detached review usage/cost will no longer naturally aggregate into the parent agent unless we add explicit plumbing for it
    - detached children no longer participate in the unified parent/child event stream
    - any behavior that depends on "being a real subagent" is bypassed
  - Because of those semantic differences, this is a reasonable middle-ground only if we want a targeted fix without designing a general visibility policy yet.

## Decisions

- No implementation choice yet; waiting for the user to pick one of the three options above.
- Recommendation:
  - Option 1 is the best long-term shape if we expect more machine-readable subagents.
  - Option 2 is the fastest if the immediate priority is just "stop showing the review JSON wall".
  - Option 3 is a viable middle ground if we want to avoid UI hacks but also avoid touching the core subagent/event APIs right now.

## Review

Not run yet.

## Summary
