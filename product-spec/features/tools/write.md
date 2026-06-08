# `write`

`write` lets an agent create a file or replace a file's complete contents.

It is mainly for models that do not use `apply_patch`. OpenAI models should normally prefer `apply_patch` for file edits because patches preserve edit intent and can cover updates, moves, and deletes in one operation.

## Availability

- Available in generic agents through the virtual `edit_files` tool group.
- Available in package-mode agents through the virtual `edit_files` tool group.
- Available in orchestrator and delegated package agents when their toolset includes file editing.

## Behavior

- The agent supplies one file path and the complete content to write.
- Relative paths are resolved from the sandbox dir.
- If the file does not exist, the tool creates it.
- If the file already exists, the tool replaces its full contents.
- Parent directories are created when needed.
- The tool authorizes the target path before writing.
- On success, the filesystem reflects the supplied content exactly.
- On parameter, authorization, or filesystem failure, the tool returns an error and should not present the write as applied.

## Inputs

- `path`: file path, absolute or sandbox-relative.
- `content`: complete file content to write. Empty content is valid.
- `request_permission`: optional boolean; asks the user for approval when the target is outside the current automatic authorization boundary.

## Output

On success, the tool returns a result indicating that the file was written and identifying the written path.

In package-mode agents, successful writes run package post-checks such as diagnostics and configured lint fixes. Post-check output may be appended to the result, and post-check failures should be visible without hiding the successful filesystem write.

Errors include invalid parameters, denied permissions, invalid paths, directory paths, parent-directory creation failures, and write failures.

## Presentation

Human-facing output presents the operation as a semantic diff, not as raw tool JSON.

The presentation should make the created or replaced file visible enough for the user to understand the edit at a glance.

## Permissions

Writes are authorized before the file is created or replaced.

In package mode, `write` reinforces the selected package boundary and then runs package-aware checks after successful writes.
