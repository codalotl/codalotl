# `delete`

`delete` lets an agent remove one file from the sandbox or from another user-authorized location.

It is mainly for models that do not use `apply_patch`. OpenAI models should normally delete files through `apply_patch` instead.

## Availability

- Available in generic agents through the virtual `edit_files` tool group.
- Available in package-mode agents through the virtual `edit_files` tool group, where ordinary deletes are scoped by the selected package code unit.
- Available in orchestrator and delegated package agents when their toolset includes file editing.

## Behavior

- The agent supplies one file path.
- Relative paths are resolved from the sandbox dir.
- The path must resolve to an existing file.
- The tool fails when the path does not exist or points to a directory.
- The tool authorizes the target path before deleting it.
- On success, the tool removes the file from the filesystem and returns a deletion result.
- On invalid input, authorization failure, or filesystem failure, the tool returns an error and should not present the deletion as applied.

## Inputs

- `path`: file path, absolute or sandbox-relative.
- `request_permission`: optional boolean; asks the user for approval when the target is outside the current automatic authorization boundary.

## Output

On success, the tool returns a result indicating that the file was deleted.

Errors include invalid parameters, missing paths, directory paths, denied permissions, and filesystem removal failures.

## Presentation

Human-facing output presents a successful delete as:

```text
• Delete path/to/file.go
```

The presentation should show the path the user can recognize. It should not show raw tool JSON.

## Permissions

Writes are authorized before the file is removed.

In package mode, `delete` reinforces the selected package boundary.
