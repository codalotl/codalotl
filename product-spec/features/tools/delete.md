# `delete`

`delete` removes one file. Some LLMs are optimized for `write`/`delete`/`edit`, and others for `apply_patch` - the LLM is either given the trio, or `apply_patch`.

It follows the ~same specification as `apply_patch` (other than the inputs).

## Inputs

- `path`: file path, absolute or sandbox-relative.
- `request_permission`: optional boolean; asks the user for approval when the target is outside the current automatic authorization boundary.

## Behavior

- The agent supplies one file path.
- Relative paths are resolved from the sandbox dir.
- The path must resolve to an existing file.
- Directories are not deleted through this tool.
- On success, the file is removed from the filesystem.
- Failed deletions are not presented as applied changes.
