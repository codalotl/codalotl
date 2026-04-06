# PR

## User Summary (do not modify)

Add an /orchestrate command to the TUI. See internal/agentbuilder/data/pr-orchestrator.prompt.md for the expected workflow. Yes, you're an orchestrator working on orchestrator. Yodawg.
- make sure review and implement tools approximiately work, i haven't tried them yet.
- the text "pr-orchestrator" is not meant to be user visible copy.

2026-04-06, 7:26am:
- add requirement to support in noninteractive.
    - add flag, like `codalotl exec --slash-command="orchestrate"`, also support `codalotl exec --slash-command="/orchestrate"`
- make sure you manually test this, don't just rely on `go test`

## Plan

### internal/tui [DONE]

- Verify `/orchestrate` resets into a fresh generic-mode orchestrator session and preserves normal follow-up chat behavior.
- Remove internal agent-name wording from user-visible copy while keeping `/orchestrate` discoverable.
- Cover the session flow and welcome/help copy with focused TUI tests.

### internal/agentbuilder

- Verify the embedded orchestrator agent is registered with the expected prompt and toolset.
- Add focused tests for the built-in `review` tool using a stubbed external command so command/arg wiring is exercised.
- Add focused tests for the built-in `implement` tool by invoking the registry-built subagent tool and asserting target package resolution and forwarded instructions.

### Validation

- Run focused tests for `internal/tui` and `internal/agentbuilder`.

## Decisions

- `/orchestrate` is the user-facing command and mode name; the internal agent identifier stays implementation-only.
- Existing preliminary branch work should be validated and fixed forward rather than reimplemented from scratch.

## Learnings

- 2026-04-06: A broad `internal/agentbuilder` implementation request produced no code diff. The next pass should stay narrowly scoped to explicit `review` and `implement` tool invocation tests.

## Review

## Summary
