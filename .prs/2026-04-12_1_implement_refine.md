# PR

## User Summary (do not modify)

Problem statement:

During the previous PR (format_events_v3), we introduced a problem with the implement tool formatter. Basically, we duplicate the message to the user:

```
• Investigating in path/to/pkg
  └ Find out..
    Also don't forget to...
  • (... various subagent events ...)
  • I found...
    I did not forget to...
• Investigated in path/to/pkg
  └ I found...
    I did not forget to...
```

So, change the design of the preset. The result event should have no summary. (see internal/agentbuilder)

## Plan

### internal/agentbuilder

- Refine the subagent-QA presenter preset so result presentations for `implement` do not emit a duplicate summary line when the result body already carries the useful text.
- Keep the call-side summary/body shape intact and preserve other presenter behaviors unless they are coupled to this bug.
- Update focused presenter/config tests to cover the new result shape and the formatter-visible output.

### internal/noninteractive integration expectations

- If this changes recorded human-readable output for orchestrator/subagent flows, update the affected replay-backed expectations to match the intended new presentation.
- Verify the change does not accidentally alter unrelated tool-call/result formatting.

### Validation

- Run focused tests for `internal/agentbuilder` and any affected noninteractive/integration coverage.

## Review

## Summary
