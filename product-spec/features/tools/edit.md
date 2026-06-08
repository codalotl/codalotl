# `edit`

`edit` lets an agent update an existing file by replacing matching text.

It is mainly for models that do not use `apply_patch` as well. OpenAI models should usually prefer `apply_patch`; other models may use `edit`, `write`, and `delete` for simpler file operations.

## Availability

- Available in generic agents through the virtual `edit_files` tool group.
- Available in package-mode agents through the virtual `edit_files` tool group.
- Available in orchestrator and delegated package agents when their toolset includes file editing.

## Behavior

- The agent supplies one file path, text to find, replacement text, and whether to replace one match or all matches.
- Relative paths are resolved from the sandbox dir.
- The path must resolve to an existing file.
- The text to find must be non-empty and present in the file.
- By default, the replacement must identify one unambiguous match. When `replace_all` is true, every matching occurrence is replaced.
- The tool authorizes the target path before editing it.
- On success, the tool edits the filesystem and returns the edited path.
- On invalid parameters, missing text, authorization failure, or replacement failure, the tool returns an error and should not present the change as applied.

## Inputs

- `path`: file path, absolute or sandbox-relative.
- `old_text`: non-empty text to find in the file.
- `new_text`: replacement text. It may be empty.
- `replace_all`: optional boolean; when true, replaces every match of `old_text`. When false or omitted, replaces one unambiguous match.
- `request_permission`: optional boolean; asks the user for approval when the target is outside the current automatic authorization boundary.

## Output

On success, the tool returns the edited file path.

In package-mode agents, successful edits run package post-checks such as diagnostics and configured lint fixes. Post-check output may be appended to the result, and post-check failures should be visible without hiding the successful filesystem edit.

Errors include invalid parameters, missing files, directory paths, unreadable files, denied permissions, absent `old_text`, ambiguous single-match replacements, and failed post-edit checks.

## Presentation

Human-facing output presents the operation as a semantic diff, not as raw tool JSON.

The presentation should show the edited path and the meaningful removed and added lines so the user can understand the change at a glance.

## Permissions

Writes are authorized before the replacement is applied.

In package mode, `edit` reinforces the selected package boundary and then runs package-aware checks after successful edits.
