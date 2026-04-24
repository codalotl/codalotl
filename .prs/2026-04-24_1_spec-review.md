# PR

## User Summary (do not modify)

background: this repo is 'codalotl', a Go-focused coding agent. There's an "orchestrator" mode -- see internal/agentbuilder for its prompt. It's designed to take an idea to a mergeable state in steps, committing work as it goes. In fact, you may be reading this **as** codalotl in orchestrator mode -- welcome to self-improvement (alternatively: you might be reading this as codex. we love you too).

Part of what the orchestrator does is edit SPEC.md files. Unfortunately it does it poorly. Part of this is that I could just do a better job describing them in the $spec-md skill. But even so, since SPEC.md files are so central to the design of codalotl, I want to do a really good job making AI edit them correctly. So, I want to create a tool that reviews changes to these files and gives feedback to the orchestrator.

In this PR:
- Define a new tool, `review_spec_changes`, which accepts a package and a message. The message might be something like "review the uncommitted changes to SPEC.md in your package. broader context: @.prs/2026-02-01_1_some-feature.md. Other specifics: abc".
- This tool launches a subagent in the package in package mode.
    - It has the $spec-md skill so it can use skill_shell to run git diff to find changes
- Just put a placeholder prompt in for now (I'll personally edit later). Something like "review SPEC.md's latest changes for best practices indicated by $spec-md, given the context in the user's message".
- Add the tool to the orchestrator agent
- Edit the orchestrator workflow as follows:
    - in the planning step, orchestrator is currently (before this PR) instructed to edit and commit the spec.
    - instead, it will iteratively edit spec files, get feedback, revise, get feedback, etc. When its satisfied, it commits the spec.md file All of this in the same planning step (not multiple turns).

## Plan

### Package internal/agentbuilder
- Add a `review_spec_changes` YAML subagent tool to the built-in agentbuilder config.
    - Parameters: `package` and `message`.
    - Launch one package-mode subagent for the requested package.
    - Use `limited_package_mode` so the reviewer has package context, `$spec-md`, and `skill_shell`, but is guided toward feedback rather than implementation.
    - Return plain-text feedback through the existing `subagent_q_and_a` presenter.
- Add `review_spec_changes` to the `pr-orchestrator` tool list.
- Update the PR orchestrator prompt's planning workflow:
    - For every `SPEC.md` change the orchestrator makes during planning, call `review_spec_changes` for that package.
    - Revise the spec and repeat review as needed within the same planning step.
    - Commit only after the orchestrator is satisfied with the final PR file and `SPEC.md` state.
    - Warn that `review_spec_changes` will usually return feedback, may be wrong, and requires orchestrator judgment about when to stop iterating.
- Update agentbuilder tests that assert built-in YAML agent/tool structure and orchestrator tool lists.
- Run `go test ./internal/agentbuilder`.

## Review

## Summary

## State

- Scope is `internal/agentbuilder`: built-in YAML config, PR orchestrator prompt, agentbuilder tests, and `internal/agentbuilder/SPEC.md`.
- `review_spec_changes` is feedback-only, one package per call, and should be used for every orchestrator-authored `SPEC.md` change during planning.
