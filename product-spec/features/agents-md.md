# AGENTS.md

Codalotl reads `AGENTS.md` files as agent-facing project instructions.

These files let users put coding-agent guidance near the code it applies to: build steps, testing rules, style conventions, package notes, and repo-specific workflow expectations.

## Discovery

Codalotl looks for `AGENTS.md` from current working context upward to the sandbox dir.

Generic sessions use sandbox context.

Package-mode sessions use selected package context.

When multiple files are found, nearer files are more specific. The agent receives enough path metadata to understand where each instruction came from.

Empty or whitespace-only `AGENTS.md` files are ignored.

## Usage

`AGENTS.md` content is added to agent context before the agent works.

Package-mode context includes applicable `AGENTS.md` instructions alongside generated package context.

Users can rely on `AGENTS.md` for durable project instructions that should not be repeated in every prompt.

## Boundaries

`AGENTS.md` is instruction context, not code.

It can guide agent behavior, but it does not grant filesystem permissions or override tool authorization.

Conflicting instructions should be resolved by normal instruction precedence and locality: more specific project instructions can refine broader project guidance.
