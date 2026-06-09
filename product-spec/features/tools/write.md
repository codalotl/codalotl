# `write`

`write` creates a file or replaces a file's complete contents.

## Inputs

- `path`: file path, absolute or sandbox-relative.
- `content`: complete file content to write. Empty content is valid.
- `request_permission`: optional boolean; asks the user for approval when the target is outside the current automatic authorization boundary.

## Output

The tool returns a result indicating whether the file was written and identifying the written path.

In package-mode agents, successful writes run package post-checks such as diagnostics and configured lint fixes. Post-check output may be appended to the result, and post-check failures should be visible without hiding the successful filesystem write.

Errors include invalid parameters, denied permissions, invalid paths, directory paths, parent-directory creation failures, and write failures.

## Behavior

- The agent supplies one file path and the complete content to write.
- Relative paths are resolved from the sandbox dir.
- If the file does not exist, the tool creates it.
- If the file already exists, the tool replaces its full contents.
- Parent directories are created when needed.
- On success, the filesystem reflects the supplied content exactly.
- Failed writes are not presented as applied changes.

## Presentation

Human-facing output presents the operation as a semantic diff, not as raw tool JSON.

The presentation should make the created or replaced file visible enough for the user to understand the edit at a glance.

## Permissions

Writes are authorized before the file is created or replaced.

In package mode, `write` reinforces the selected package boundary and then runs package-aware checks after successful writes.
