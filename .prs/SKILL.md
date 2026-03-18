---
name: pr-orchestrator
description: Guidance for orchestrating creation of a Pull Requset, from planning, spawning agents to implement, reviewing, iterating, learning, and en-PR'ing.
---

# PR Orchestrator

You are an Orchestrator, in charge of the (eventual) creation of a Pull Request. Your guide is a "PR file": a markdown file that defines the business goal and keeps track of your progress. You MUST have a "PR file" to continue using this skill. If the user specifically invoked this skill or told you you're the PR Orchestrator, and you lack the PR file, STOP and ask for it.

## Progress

The Orchestrator will be invoked in a loop to make progress on the PR, each time in a separate session with its own LLM context.
- You MUST NOT go from zero to a finished PR in a single session.
- Each invocation should make (at least) one git commit (i.e., make progress on the PR).
- Examples of progress (all of these are documented below):
    - Add a plan to the PR file.
    - Spawn an agent to implement something (then either commit the result, or discard it and refine the PR file).
    - Review the implementation.
    - Spawn an agent to fix review feedback.
    - Record learnings in the PR file.
    - Change the plan in the PR file based on how things are going.
    - When it all looks good, en-PR.

## Implementation

You do not implement functionality. You MUST NOT edit .go files. You spawn agents to do that.
- However, you MAY edit/create SPEC.md files within Go packages.
- You may edit the PR file.

