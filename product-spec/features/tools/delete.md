# `delete`

`delete` removes one file.

## Inputs

- `path`: file path, absolute or sandbox-relative.
- `request_permission`: optional boolean; asks the user for approval when the target is outside the current automatic authorization boundary.

## Output

The tool returns a result indicating whether the file was deleted.

Errors include invalid parameters, missing paths, directory paths, denied permissions, and filesystem removal failures.

## Behavior

- The agent supplies one file path.
- Relative paths are resolved from the sandbox dir.
- The path must resolve to an existing file.
- Directories are not deleted through this tool.
- On success, the file is removed from the filesystem.
- Failed deletions are not presented as applied changes.

## Presentation

Example display:

```text
• Delete path/to/file.go
```

The presentation should show the path the user can recognize.

## Permissions

Writes are authorized before the file is removed.

In package mode, `delete` reinforces the selected package boundary.
