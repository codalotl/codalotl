# Package Mode

Package mode is Codalotl's main Go-optimized coding mode. The user selects one Go package, then gives ordinary coding instructions. Codalotl gives the agent package-specific context, package-scoped tools, and cross-package levers that let it work in a larger Go module without treating the whole repository as one flat workspace.

Package mode is meant for focused package work: implementing behavior in one package, fixing tests in one package, improving its public API, or coordinating a small change across packages through subagents. Generic mode remains the better fit for broad repository questions, planning, and work that is not centered on a single Go package.

## Entry Points

The user can enter package mode from the TUI:

```text
/package <path/to/pkg>
```

The user can run one noninteractive package-mode turn from the CLI:

```bash
codalotl exec --package <path/to/pkg> "implement xyz"
```

Package arguments follow `features/cli.md` package argument semantics. The selected package must resolve inside the sandbox dir for ordinary user-facing package mode.

In the TUI, entering package mode starts a new session. `/new` while in package mode starts a new session but keeps the selected package. `/package` with no argument and `/generic` exit package mode and start a generic session.

## Package Selection

Package mode is currently for Go packages.

The selected path may be:
- An import path.
- A relative or absolute package directory.
- `.` for the current directory.

The selected directory must exist and be a directory. It should normally load as a Go package, but package mode can still start for an existing directory inside a Go module that does not yet load as a package, enabling its use for new and broken packages.

## Initial Context

Package-mode sessions start with generated Go context for the selected package. This context is part of the agent's initial conversation before it acts on the user's request.

Initial context includes:
- Package identity, including module path, package path, and import path when available.
- A package file listing.
- Package maps for non-test code and test code, showing declarations and where they live without full function bodies.
- Packages that use the current package, when available.
- Diagnostics status.
- Test status.
- Lint status.
- Applicable `AGENTS.md` instructions.

This context lets the agent begin with a compact understanding of the package's shape and health instead of spending its first steps listing directories, grepping for declarations, and discovering basic test failures.

In the TUI, package context may take time to gather because it can run package analysis, tests, and lints. The UI shows context-gathering progress. The user may type while context gathering is in progress; the gathered context should still be included before the agent acts on the user's message.

## File Boundaries

Package mode narrows direct file access to the selected package's code unit. This is a UX and agent-guidance boundary, not a security sandbox.

The package code unit is rooted at the selected package directory:
- The base package directory is included.
- Subdirectories are included recursively when they do not contain Go files.
- A `testdata` directory directly under an included directory is included entirely, even when it contains Go fixture files.
- Nested Go packages are excluded from direct package-mode read/write/list access.

This gives the agent normal access to package files and supporting fixtures while discouraging direct edits across unrelated packages. If the user explicitly supplies outside context, such as by mentioning files or directories, the agent may use that context according to the surrounding agent and authorization rules.

## Tools

A package-mode agent has package-scoped file reading, listing, and editing tools. It does not have the generic raw shell tool.

Instead, package mode provides Go-aware tools such as:
- `diagnostics`: inspect Go diagnostics for the package.
- `fix_lints`: apply configured lint fixes.
- `run_tests`: run tests for the selected package.
- `run_project_tests`: run broader project tests when needed.
- `module_info`: inspect module/package information.
- `get_public_api`: read compact public API documentation for another package.
- `clarify_public_api`: ask a read-only subagent to explain another package's API from evidence.
- `get_usage`: inspect packages that use the selected package.
- `update_usage`: update packages that use the selected package.
- `change_api`: update a package imported by the selected package.

Package-mode editing tools should automatically apply normal Go formatting and configured post-edit checks where practical, so ordinary edits converge toward buildable, lint-clean Go code without requiring the agent to remember every mechanical cleanup step.

Package mode may also expose skill-backed command execution. This is for commands provided by skills and package workflows, not general-purpose shell exploration.

## Cross-Package Work

Package mode should make one package feel small and tractable without making multi-package Go work impossible.

For read-only cross-package understanding, the agent should prefer `get_public_api` over reading another package's source. Public API context is usually cheaper, clearer, and closer to how package boundaries should be understood.

When public API docs are insufficient, the agent can use `clarify_public_api` to ask a focused read-only subagent a specific question about another package. Clarification answers can be recorded and later used by documentation-improvement workflows.

When code outside the selected package must change, the agent should use package-aware subagent tools:
- `change_api` for changes to an imported package needed by the current package.
- `update_usage` for downstream packages that use the current package.

Those subagents run with their own package-mode context, preserving the main agent's context window and keeping each edit centered on one package.
