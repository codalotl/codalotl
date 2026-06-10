# `write`

`write` creates a file or replaces a file's complete contents. Some LLMs are optimized for `write`/`delete`/`edit`, and others for `apply_patch` - the LLM is either given the trio, or `apply_patch`.

It follows the ~same specification as `apply_patch` (other than the inputs).

## Inputs

- `path`: file path, absolute or sandbox-relative.
- `content`: complete file content to write. Empty content is valid.
- `request_permission`: optional boolean; asks the user for approval when the target is outside the current automatic authorization boundary.

## Behavior

- The agent supplies one file path and the complete content to write.
- Relative paths are resolved from the sandbox dir.
- If the file does not exist, the tool creates it.
- If the file already exists, the tool replaces its full contents.
- Parent directories are created when needed.
- On success, the filesystem reflects the supplied content exactly.
- Failed writes are not presented as applied changes.