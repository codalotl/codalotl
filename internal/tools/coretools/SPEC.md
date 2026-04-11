# coretools

The coretools package provides basic tools that any agent can use: things like reading and writing to files, directory listing, and shell access.

## Model-Specific Tools

Some LLMs are trained to use specific tools, and will do so better than other LLMs:
- OpenAI models should use `apply_patch` (which supports editing, deleting, and moving files).
- Other models should use `write`, `edit`, and `delete`.

## Presentations

- Each tool should have a `Presenter`.
- This SPEC.md specifies them in a concise, ambiguous way. Example:
    - A read_file tool that presents as
        - `{Behavior: Replace, Summary: {JoinWithSpace: true, Segments: [{Text: "Read", Role: Action}, {Text: "path/to/file.go", Role: Normal}]}}` (NOTE: this is pseudo-code)
    - Can just be specified here as:
        - `Read path/to/file.go`

## Tools

(not all tools are reflected here yet)

### read_file

Presentation: `Read path/to/file.go`

### edit

The `edit` tool edits files by find and replace. Applies it with `applypatch.Replace`.
- Afterwards, runs diagnostics (checks for build errors) and configured lints.

Params:
- `path`: absolute or relative
- `old_text`: old text to find in the file
- `new_text`: new text to replace it with
- `replace_all`: bool (default: false) - replace all occurances of old_text with new_text.

### write

The `write` tool creates a new file with content, or replaces it with content if it already exists.
- Afterwards, runs diagnostics (checks for build errors) and configured lints.

Params:
- `path`: absolute or relative
- `content`: content to write

### delete

The `delete` tool deletes the file.

Params:
- `path`: absolute or relative
