# `ls`

`ls` lists a directory.

## Inputs

- `path`: directory path, absolute or sandbox-relative.
- `request_permission`: optional boolean; asks the user for approval when the target is outside the current automatic authorization boundary.

## Output

The tool returns a structured directory listing with the listed directory path and one entry per visible child.

Errors include invalid parameters, missing paths, file paths, unreadable directories, and denied permissions.

Example output:

```text
<ls ok="true" cwd="/home/someuser/proj">
$ ls -1p
doc.go
go.mod
go.sum
internal/
main.go
</ls>
```

## Behavior

- The agent supplies one directory path to list.
- Relative paths are resolved from the sandbox dir.
- `.` lists the sandbox dir.
- The path must resolve to an existing directory.
- Hidden files and directories, whose names start with `.`, are omitted from the listing.
- Directory entries are marked with a trailing slash so the agent can distinguish files from directories.
- The result includes the absolute directory that was listed, so the agent can disambiguate relative paths.

## Presentation

Example display:

```text
• List path/to/dir
```

## Permissions

Directory reads are authorized before listing the directory.

In package mode, `ls` reinforces the selected package boundary: the agent can directly list directories in the selected package code unit, while outside listings require explicit authorization or context supplied through other Go-aware tools.
