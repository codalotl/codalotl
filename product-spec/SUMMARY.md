# Summary

Codalotl, or `codalotl` (the binary), is an AI-powered software construction system ("agent") specifically for Go projects.

It is a TUI and CLI.

## Index

- Read `PRODUCT-SPEC-GUIDANCE.md` to understand how the product spec works.
- Read `PHILOSOPHY.md` to understand design choices.
- `features/` contains individual features.

## Environment

### OSes

- Runs on Mac and Linux (including WSL), in a terminal.

### Go

- Only works with Go version >= 1.18
- Is designed to work with `go.mod` files. A Go project without a module is unsupported.
- Requires certain Go-related installed programs installed:
    - go, gopls, goimports, gofmt

### Git

- Basically requires the git binary, and that the software we're building is version controlled with git.
- Certain parts of the codebase may gracefully degrade without git or a git repo - that's fine.
- But we certainly don't try to equally support git and non-git. Certain operations may simply fail without git and a repo. Behavior without a git repo is undefined unless otherwise stated.

### Sandbox Dir

- This is the CWD when starting the TUI and CLI in agent modes. The agent is intended to be mostly isolated to this dir tree.
    - It can still read shared Go deps outside this dir (ex: deps mentioned in `go.mod` and stdlib deps like `fmt`).
    - It can write to shared Go cache dir.
    - User permission can let it read/write outside this dir.
    - Certain non-agent commands like `codalotl context initial .` can be run from any package.
- MUST contain a git repo (in the same dir).
- MUST contain a `go.mod` file (but this can be in a subdir).

## Multi-module Go projects

- The sandbox dir MUST fully contain project's `go.mod` file. But it need not be the same dir.
- There can be nested `go.mod` files or multiple peer `go.mod` files.

