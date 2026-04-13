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

So, change the design of the preset. The result event should have no body, but should keep its summary line. (see internal/agentbuilder)

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
- Change SPEC.md.
- Keep the implement tool's completion summary (`Implemented <path>`) but stop repeating the nested subagent answer in the completion body.

## Review

## Summary
