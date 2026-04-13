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
