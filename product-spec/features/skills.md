# Skills

Codalotl supports Agent Skills: named instruction bundles that teach agents a workflow, domain, tool, or project convention without baking every detail into Codalotl's core prompts.

## Skill Format

A skill is a directory containing a `SKILL.md` file.

`SKILL.md` uses the Agent Skills format:
- Required YAML frontmatter with `name` and `description`.
- Optional metadata like `license`, `compatibility`, and custom metadata.
- Markdown body containing the instructions the agent reads when the skill is activated.
- Optional supporting directories like `scripts/`, `references/`, and `assets/`.

The skill name is the user-facing identifier. It must match the skill directory name and follow Agent Skills naming rules: lowercase letters, numbers, and hyphens; no leading or trailing hyphen.

The description is agent-facing discovery text. It should explain what the skill does and when to use it, because Codalotl includes descriptions in the agent's initial skills list.

## Discovery

Codalotl discovers skills from project and user locations.

Starting from the session's relevant directory, Codalotl looks upward for `.codalotl/skills` directories. It also looks in the user's `~/.codalotl/skills` and `~/.codalotl/skills/.system` directories.

In package mode, discovery starts from the selected package context. In generic mode, discovery starts from the sandbox dir.

Project skills let a repository define local workflows and conventions. User skills let a developer keep personal capabilities available across projects. System skills are built in and installed by Codalotl.

Skills are discovered non-recursively within each skill search directory: each direct child directory with a `SKILL.md` is a candidate skill.

## Built-in Skills

Codalotl ships with system skills that support its opinionated Go workflows:
- `spec-md`: guidance for creating, editing, reviewing, and implementing Go packages from `SPEC.md`.
- `skill-creator`: guidance for creating or updating Codalotl skills.
- `go-testing`: guidance for Go test quality, maintainability, coverage, and useful test commands.

System skills are installed under `~/.codalotl/skills/.system`. Codalotl may update its own system skills when the installed copy differs from the bundled copy, but should not delete or modify unrelated user-installed skills.

## Agent Context

When skills are enabled for an agent, Codalotl adds a skills section to the agent's initial context.

That context includes:
- A short explanation of skills.
- Available skill names, descriptions, and `SKILL.md` locations.
- Instructions for when and how the agent should read and use a skill.

The full body of every skill is not loaded into the agent context up front. The agent reads a skill's `SKILL.md` when the user names the skill or the current task matches the skill description.

If a skill references supporting files, the agent may read those files when needed. Skill authors should use relative references from the skill root so agents can find supporting material reliably.

## Usage

Users can invoke a skill explicitly by naming it, often with `$skill-name`, or by giving a task that clearly matches a skill's description.

Examples:

```text
Use $spec-md to review this package SPEC.md
```

```text
Create a skill for our database migration workflow
```

Agents should use the minimal relevant skill set for the turn. If a named skill is unavailable or invalid, the agent should say so briefly and continue with the best available fallback when possible.

Skills guide the agent; they do not create a new product mode by themselves. The same TUI, `exec`, package-mode, permission, and tool behavior still applies.

## Skill Commands

Skills may include scripts or command instructions. Agents can run these only through the command tools available in their current agent.

In generic mode, a skill may direct use of ordinary shell access when the generic agent has the `shell` tool.

In package mode, raw shell access is intentionally unavailable. Skill-backed command execution uses `skill_shell`, which requires the agent to name the skill that authorized the command. This lets package-mode workflows run skill-provided commands while preserving the package-mode bias toward Go-aware tools and scoped file access.

When a dedicated Codalotl tool exists, a skill should usually prefer that tool over shell commands. For example, a Go testing skill can explain coverage commands while still preferring `run_tests` for ordinary package test runs.

## `/skills`

The TUI provides:

```text
/skills
```

This lists installed valid skills visible to the current session.

It also reports skill discovery and validation issues, such as invalid frontmatter, invalid names, mismatched directory names, or unreadable `SKILL.md` files. These issues should be visible to users without preventing an otherwise usable session from starting, unless the failure blocks required startup behavior.

`/skills` is informational. It does not install, edit, activate, or remove skills.

## Errors

Invalid skills are ignored for agent capability purposes and surfaced as user-visible issues.

A malformed or invalid skill should not silently appear in the agent's available skills list. The user should be able to inspect what failed and fix the skill directory or `SKILL.md`.

Skill discovery errors are normally non-fatal. Codalotl should still let the user work with other available skills or no skills when that is practical.
