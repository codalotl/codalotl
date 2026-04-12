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

### ls

Presentation: `List some/path`

### shell

Presentation:
- In progress: `Running go test .`
- Complete: `Ran go test .`
- Summary uses command argv, not cwd/timeout/request-permission metadata.
- The command's output is the body. If there is no output, There is no body.
- If the output is more than 5 lines, show the first five, followed by `… +13 lines` (for example).

### skill_shell

Presentation: same behvior as shell.

### update_plan

Presentation:
- Summary: `Update Plan`
- Body:
    - A single checklist block
    - Optional checklist overview line first, when non-empty
    - Then plan items in order, one per line
- Plan items:
    - Completed: `✔ Some step`
    - Pending: `□ Some step`
    - In progress: `□ Some step`
- The first uncompleted item is highlighted as next-up work.
- Explicit `in_progress` items are also emphasized.
- Shared formatter error handling still owns tool errors.

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

Presentation: `Delete path/to/file.go`

The `delete` tool deletes the file.

Params:
- `path`: absolute or relative
