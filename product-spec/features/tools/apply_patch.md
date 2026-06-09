# `apply_patch`

`apply_patch` edits files by applying a structured patch.

## Inputs

The tool input is patch text:

```text
*** Begin Patch
*** Update File: path/to/file.go
@@
-old text
+new text
*** End Patch
```

- `request_permission`: optional boolean; asks the user for approval when the patch targets material access outside the normal authorization boundary.

## Output

The tool returns a structured result indicating whether the patch was applied and listing changed files.

In package-mode agents, successful edits run package post-checks such as diagnostics and configured lint fixes. Post-check output may be appended to the result, and post-check failures should be visible without hiding the successful filesystem edit.

Errors include invalid patch text, missing paths, conflicts, denied permissions, and failed filesystem writes.

## Behavior

- The agent submits one patch containing one or more file operations.
- File operations can add files, update files, delete files, and move files.
- Patch paths are sandbox-relative.
- The patch grammar uses file-oriented sections rather than line-numbered diffs.
- The tool applies the patch atomically enough that failed parses, authorization failures, and conflicts are not presented as applied edits.

## Presentation

Human-facing output presents the operation as a semantic diff, not as raw tool JSON.

The presentation should make created, modified, moved, and deleted files visible enough for the user to understand the edit at a glance.

## Permissions

Writes are authorized before the patch is applied.

In package mode, `apply_patch` reinforces the selected package boundary and then runs package-aware checks after successful edits.
