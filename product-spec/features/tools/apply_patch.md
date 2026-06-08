# `apply_patch`

`apply_patch` lets an agent edit files by submitting a structured patch.

It is the preferred file-editing tool for OpenAI models. Other models may use `edit`, `write`, and `delete` instead.

## Availability

- Available in generic agents through the virtual `edit_files` tool group.
- Available in package-mode agents through the virtual `edit_files` tool group.
- Available in orchestrator and delegated package agents when their toolset includes file editing.

## Behavior

- The agent submits one patch containing one or more file operations.
- File operations can add files, update files, delete files, and move files.
- Patch paths are sandbox-relative.
- The patch grammar uses file-oriented sections rather than line-numbered diffs.
- The tool authorizes all affected paths before applying the patch.
- On success, the tool edits the filesystem and returns the changed file list.
- On parse, authorization, conflict, or apply failure, the tool returns an error and should not present the change as applied.

## Inputs

For freeform-capable models, the tool input is the patch text itself:

```text
*** Begin Patch
*** Update File: path/to/file.go
@@
-old text
+new text
*** End Patch
```

For function-call models, the input has:

- `patch`: patch text using the same grammar.
- `request_permission`: optional boolean; asks the user for approval when the patch targets material access outside the normal authorization boundary.

## Output

On success, the tool returns a structured result indicating that the patch was applied and listing changed files.

In package-mode agents, successful edits run package post-checks such as diagnostics and configured lint fixes. Post-check output may be appended to the result, and post-check failures should be visible without hiding the successful filesystem edit.

## Presentation

Human-facing output presents the operation as a semantic diff, not as raw tool JSON.

The presentation should make created, modified, moved, and deleted files visible enough for the user to understand the edit at a glance.

## Permissions

Writes are authorized before the patch is applied.

In package mode, `apply_patch` reinforces the selected package boundary and then runs package-aware checks after successful edits.
