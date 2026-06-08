# `ls`

`ls` lets an agent inspect the immediate contents of a directory in the sandbox or in another user-authorized location.

## Availability

- Available in generic agents.
- Available in package-mode agents, where ordinary listings are scoped by the selected package code unit.
- Available to read-only helper agents when directory discovery is part of their allowed context gathering.

## Behavior

- The agent supplies one directory path to list.
- Relative paths are resolved from the sandbox dir.
- `.` lists the sandbox dir.
- The path must resolve to an existing directory.
- Hidden files and directories, whose names start with `.`, are omitted from the listing.
- Directory entries are marked with a trailing slash so the agent can distinguish files from directories.
- The result includes the absolute directory that was listed, so the agent can disambiguate relative paths.

## Inputs

- `path`: directory path, absolute or sandbox-relative.
- `request_permission`: optional boolean; asks the user for approval when the target is outside the current automatic authorization boundary.

## Output

The tool returns a structured directory listing with the listed directory path and one entry per visible child.

Errors include invalid parameters, missing paths, file paths, unreadable directories, and denied permissions.

## Presentation

Human-facing output presents a successful listing as:

```text
• List path/to/dir
```

The presentation should show the path the user can recognize. It should not dump the directory listing into the chat-style progress line; the listing belongs to the agent-facing result.

## Permissions

Directory reads are authorized before listing the directory.

In package mode, `ls` reinforces the selected package boundary: the agent can directly list directories in the selected package code unit, while outside listings require explicit authorization or context supplied through other Go-aware tools.
