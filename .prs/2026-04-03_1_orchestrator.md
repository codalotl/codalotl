# PR

## User Summary (do not modify)

Add an /orchestrate command to the TUI. See internal/agentbuilder/data/pr-orchestrator.prompt.md for the expected workflow. Yes, you're an orchestrator working on orchestrator. Yodawg.
- don't do noninteractive yet
- make sure review and implement tools approximiately work, i haven't tried them yet.
- the text "pr-orchestrator" is not meant to be user visible copy.

## Plan

### internal/tui [DONE]

- Verify `/orchestrate` resets into a fresh generic-mode orchestrator session and preserves normal follow-up chat behavior.
- Keep this interactive-only; do not add noninteractive support in this PR.
- Remove internal agent-name wording from user-visible copy while keeping `/orchestrate` discoverable.
- Cover the session flow and welcome/help copy with focused TUI tests.

### internal/agentbuilder

- Verify the embedded orchestrator agent is registered with the expected prompt and toolset.
- Make sure `review` and `implement` are usable enough for the orchestrator workflow, especially package targeting for `implement`.
- Add or tighten registry/YAML tests around the orchestrator wiring and tool behavior.

### Validation

- Run focused tests for `internal/tui` and `internal/agentbuilder`.

## Decisions

- `/orchestrate` is the user-facing command and mode name; the internal agent identifier stays implementation-only.
- Existing preliminary branch work should be validated and fixed forward rather than reimplemented from scratch.

## Review

## Summary
