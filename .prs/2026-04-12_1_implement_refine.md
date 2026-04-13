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
- Keep the implement tool's completion summary (`Implemented <path>`) but stop repeating the nested subagent answer in the completion body.
- Keep YAML normalization and presenter behavior aligned with `SPEC.md`; no public API changes expected.
- Add or update package tests covering implement-tool call presentation vs. completion presentation.

### Validation
- Run focused `internal/agentbuilder` tests.
- Run broader formatter-facing tests if the changed presenter output affects existing expectations.

## Learnings

- A first attempt to omit the presenter summary by leaving `result_action` empty was not useful.
- `internal/agentformatter` treats presenter outputs with empty `Summary.Segments` as unpresented and falls back to the generic `Tool implement {...}` header.
- The requested output keeps the completion summary line and removes the duplicated completion body, so the next implementation step should target repeated result-body rendering rather than blanking the summary.

## Review

## Summary
