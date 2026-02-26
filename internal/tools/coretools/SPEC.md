# coretools

The coretools package provides basic tools that any agent can use: things like reading and writing to files, directory listing, and shell access.

## Model-Specific Tools

Some LLMs are trained to use specific tools, and will do so better than other LLMs:
- OpenAI models should use `apply_patch` (which supports editing, deleting, and moving files).
- Other models should use `write`, `edit`, and `delete`.

## Tools

(not all tools are reflected here yet)

### write

The `write` tool creates a new file with content, or replaces it with content if it already exists.

Params:
- `path`: absolute or relative
- `content`: content to write

### delete

The `delete` tool deletes the file.

Params:
- `path`: absolute or relative