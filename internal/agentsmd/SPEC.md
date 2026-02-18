# agentsmd

This package implements support for AGENTS.md files.

## AGENTS.md Summary (taken from https://agents.md)

Think of AGENTS.md as a README for agents: a dedicated, predictable place to provide the context and instructions to help AI coding agents work on your project.

README.md files are for humans: quick starts, project descriptions, and contribution guidelines.

AGENTS.md complements this by containing the extra, sometimes detailed context coding agents need: build steps, tests, and conventions that might clutter a README or aren’t relevant to human contributors.

We intentionally kept it separate to:
- Give agents a clear, predictable place for instructions.
- Keep READMEs concise and focused on human contributors.
- Provide precise, agent-focused guidance that complements existing README and docs.
- Rather than introducing another proprietary file, we chose a name and format that could work for anyone. If you’re building or using coding agents and find this helpful, feel free to adopt it.

## Public API

```go
// Read will read AGENTS.md files in cwd, its parent, up to sandboxDir. It returns a concatenation of all AGENTS.md files it finds, in a format that can be directly
// supplied to an LLM. This returned concatenated string may have some explanation and metadata (ex: filenames), in addition to the actual bytes from the files.
// If there are no AGENTS.md files (or they are empty), Read returns ("", nil).
//
// cwd must be in sandboxDir. They may be the same path.
//
// Example return value:
//
//	The following AGENTS.md files were found, and may provide relevant instructions. The nearest AGENTS.md file to the target code takes precedence.
//
//	AGENTS.md found at /home/user/proj/AGENTS.md:
//	<file text>
//
//	AGENTS.md found at /home/user/proj/subdir/AGENTS.md:
//	<file text>
func Read(sandboxDir, cwd string) (string, error)
```
