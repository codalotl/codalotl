# AGENTS.md

Codalotl reads `AGENTS.md` files as agent-facing project instructions.

These files let users put coding-agent guidance near the code it applies to: build steps, testing rules, style conventions, package notes, and repo-specific workflow expectations.

## Discovery

In package-mode, Codalotl looks for `AGENTS.md` in the current package, and upward to the sandbox dir, choosing the first `AGENTS.md` it finds.

In non-package-mode, Codalotl only looks in the sandbox dir.

Tools that execute subagents should usually also include `AGENTS.md` in their subagent context, except for certain focused/narrow cases like `clarify_public_api`.

Empty or whitespace-only `AGENTS.md` files are ignored.

## Usage

`AGENTS.md` content is added to agent context before the agent works, along with brief instructions about what `AGENTS.md` files are.
