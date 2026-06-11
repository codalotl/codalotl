# `delete`

`delete` removes one file. Some LLMs are optimized for `write`/`delete`/`edit`, and others for `apply_patch` - the LLM is either given the trio, or `apply_patch`.

It follows the ~same specification as `apply_patch` (other than the inputs).

## Inputs

- `path`: file path, absolute or sandbox-relative.
- `request_permission`: optional boolean; asks the user for approval when the target is outside the current automatic authorization boundary.

## Output

The tool returns a short success result naming the deleted file.

Errors include invalid parameters, missing paths, directory paths, denied permissions, and failed filesystem deletion.

Example output:

```text
Deleted file: internal/example/old_fixture.txt
```

## Behavior

- The agent supplies one file path.
- Relative paths are resolved from the sandbox dir.
- The path must resolve to an existing file.
- Directories are not deleted through this tool.
- On success, the file is removed from the filesystem.
- Failed deletions are not presented as applied changes.
