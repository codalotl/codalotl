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
