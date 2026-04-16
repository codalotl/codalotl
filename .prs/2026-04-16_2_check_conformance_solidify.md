# PR

## User Summary (do not modify)

We're working on checking conformance.

<TODO: add a brief summary of check_conformance pr>

Problem:

1. The check conformance subagents' output is not fully validated. For example, if it returns {conforms: false} without listing nonconformances, or {conforms: true, nonconformances: [...]}, both are accepted.
2. The actual nonconformances aren't printed in the final tool output. I'd like them to be.

Goal:
- Validate output of subagent. If invalid, subagent should error.
- user can see actual nonconformances printed out.

