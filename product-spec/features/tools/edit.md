# `edit`

`edit` updates an existing file by replacing matching text. Some LLMs are optimized for `write`/`delete`/`edit`, and others for `apply_patch` - the LLM is either given the trio, or `apply_patch`.

It follows the ~same specification as `apply_patch` (other than the inputs and `replace_all` support).

## Inputs

- `path`: file path, absolute or sandbox-relative.
- `old_text`: non-empty text to find in the file.
- `new_text`: replacement text. It may be empty.
- `replace_all`: optional boolean; when true, replaces every match of `old_text`. When false or omitted, replaces one unambiguous match.
- `request_permission`: optional boolean; asks the user for approval when the target is outside the current automatic authorization boundary.

## Behavior

- The agent supplies one file path, text to find, replacement text, and whether to replace one match or all matches.
- Relative paths are resolved from the sandbox dir.
- The path must resolve to an existing file.
- The text to find must be non-empty and present in the file.
- By default, the replacement must identify one unambiguous match.
- When `replace_all` is true, every matching occurrence is replaced.
- Failed replacements are not presented as applied changes.
