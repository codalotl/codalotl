# Permissions

Codalotl has a deliberately simple and relatively permissive permission system. True security is accomplished by running Codalotl in a container or other OS sandbox.

## Basics

By default, read/writes are permitted within the sandbox. Reads/writes outside the sandbox require user authorization.

The user can auto-approve authorization checks with `autoyes: true` in config. For `codalotl exec`, the user can also pass `--yes` to auto-approve checks for that run.

Authorization checks are requested at tool boundaries before the action happens. The request should identify the action being requested and the path or command involved clearly enough for the user to make a decision. If the request is approved, the tool continues. If it is denied, the tool returns a denied-permission result to the agent rather than performing the action.

In the TUI, authorization checks are shown as interactive permission prompts. In `codalotl exec`, authorization checks are noninteractive: `--yes` and `autoyes: true` approve them, and otherwise they are denied.

The permission system is not an OS-level sandbox. It is a product boundary around Codalotl's own tools and agent behavior. User code, subprocesses, language servers, Go commands, and other external programs can have their own filesystem behavior.

Go-aware workflows may read Go standard-library code and module dependency code outside the sandbox without asking for user authorization. Codalotl may also write normal Go toolchain cache data outside the sandbox, like build cache or module cache data, without asking for user authorization.

## Mentions

The user can mention files and directories with `@` to give the agent extra read context. For example, `@README.md`, `@../notes.md`, `@/absolute/path/to/file.go`, and `@"path with spaces.md"` are path mentions.

Mentions grant direct read/list access to the mentioned files and directories, even when they are outside the selected package. In package mode, this is the main way for the user to manually add specific outside-package context without switching modes.

Directory mentions grant access to files and directories under that directory. Mentioned globs can grant access to matching paths, like `@docs/*.md`.

Mentions may grant read/list access outside the sandbox when the normal sandbox policy permits user-authorized outside reads. Mentions do not grant write access.

## Package Mode

In package mode, the main agent is restricted to the package dir and its supporting dirs. The agent may not ask for authorization to read outside that dir.

The package boundary is a UX and agent-guidance boundary, not a security boundary. The main package-mode agent should directly list, read, and edit files in the selected package code unit. It should use Go-aware tools for cross-package understanding and changes instead of directly reaching into unrelated source directories.

Some tools purposefully read outside the selected package dir. For instance, `get_public_api` can inspect another package's public API, and `clarify_public_api` can ask a read-only subagent about another package. Other tools can launch package-mode agents in other dirs, like `change_api` and `update_usage`. That is allowed because those tools are the intended cross-package workflow.

## Allowed Shell Commands

No attempt is made to classsify, whitelist, or blacklist shell commands.
