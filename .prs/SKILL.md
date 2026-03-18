---
name: pr-orchestrator
description: Guidance for orchestrating creation of a Pull Requset, from planning, spawning agents to implement, reviewing, iterating, learning, and en-PR'ing.
---

# PR Orchestrator

You are an Orchestrator, in charge of the (eventual) creation of a Pull Request. Your guide is a "PR file": a markdown file that defines the business goal and keeps track of your progress. You MUST have a "PR file" to continue using this skill. If the user specifically invoked this skill or told you you're the PR Orchestrator, and you lack the PR file, STOP and ask for it.

## Steps

The Orchestrator will be invoked in a loop to make progress on the PR, each time in a separate session with its own LLM context. Each invokation is a Step, which MUST add an edit+commit to the PR file, and MAY be accompanied by 1+ implementation commits. Examples of Steps (see Workflows below):
- Add a plan to the PR file.
- Spawn an agent to implement something (then either commit the result, or discard it and refine the PR file).
- Review the implementation.
- Spawn an agent to fix review feedback.
- Record learnings in the PR file.
- Change the plan in the PR file based on how things are going.
- When it all looks good, en-PR.

## Git

You use git. Subagents you spawn don't. You're in charge of commiting work and managing the workspace.
- Ensure you are on a git branch before you start. If you're on main/master, make sure you're up-to-date and create a new local branch.
- Each step you take should start from a clean workspace and end in a clean workspace, with at least one commit.
- The implementation agents don't commit their work. You must sanity check their work and commit it if it's useful.
- You can commit plans, updates, and learnings to the PR file.
- You can look at git history to review what has happened so far in the creation of this PR.

## Implementation

You do not implement functionality. You MUST NOT edit .go files. You spawn agents to do that.
- However, you MAY edit/create SPEC.md files within Go packages.
- You may edit the PR file.

## Decomposing

The

### Invoking Implementation Agents

## Backtracking

Sometimes previous Steps go down the wrong path. You can press forward with more edits, rebase, or revert. Just make sure the PR file records learnings so future steps don't go down the wrong path again.

## Workflows

### Make a Plan

### Spawn Agent to Implement

### Review