# Context

Codalotl can print LLM-oriented Go context from the CLI.

Context commands help users inspect what Codalotl knows about packages, copy useful context into other tools, and debug package-mode startup.

## CLI

### codalotl context public <path/to/pkg>

Prints compact public API documentation for one package.

Package path follows `features/cli.md` package argument semantics.

Output is intended for humans and LLMs, not stable machine parsing.

### codalotl context initial <path/to/pkg>

Prints initial package-mode context for one Go package.

Context includes:
- package identity
- package file listing
- non-test package map
- test package map when reasonably small
- packages that use current package
- diagnostics status
- test status
- lint status
- applicable `AGENTS.md` instructions

This is approximately the context package-mode agents receive before acting on user instructions.

### codalotl context packages [--search <go_regexp>] [--deps]

Prints LLM-friendly package list for the module containing current working directory.

Options:
- `--search`: filter package list with Go regexp.
- `--deps`: include packages from direct dependency modules.

Output helps users and agents find package names before selecting package mode or using package-aware tools.

## Shape

Context output is text-first and explanation-first.

It may include structured-looking blocks, but users should treat output as prompt material rather than a stable API.

Initial context may elide very large sections, especially test maps, to preserve usefulness.

## Checks

Initial context may run diagnostics, tests, lints, and package usage lookup.

Callers may skip expensive checks in contexts where fast startup matters. When checks are skipped, output should say they were not run.
