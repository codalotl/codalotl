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

It should be:

```
• Investigating in path/to/pkg
  └ Find out..
    Also don't forget to...
  • (... various subagent events ...)
  • I found...
    I did not forget to...
• Investigated in path/to/pkg
```

## Plan

### `internal/agentbuilder`
- Update `subagent_q_and_a` presenter behavior so result events for implement-style subagent tools can omit the completion summary while still showing result body output.
- Keep YAML normalization and presenter behavior aligned with `SPEC.md`; no public API changes expected.
- Add or update package tests covering call presentation vs. result presentation for the preset.

### Validation
- Run focused `internal/agentbuilder` tests.
- If presentation fixtures outside the package fail because the event shape changed, update them in a follow-up implementation step.

## Review

## Summary
