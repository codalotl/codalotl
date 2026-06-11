# `write`

`write` creates a file or replaces a file's complete contents. Some LLMs are optimized for `write`/`delete`/`edit`, and others for `apply_patch` - the LLM is either given the trio, or `apply_patch`.

It follows the ~same specification as `apply_patch` (other than the inputs).

## Inputs

- `path`: file path, absolute or sandbox-relative.
- `content`: complete file content to write. Empty content is valid.
- `request_permission`: optional boolean; asks the user for approval when the target is outside the current automatic authorization boundary.

## Output

The tool returns a short success result naming the written file. In package mode, diagnostics and lints may be appended after a successful write.

Errors include invalid parameters, denied permissions, and failed filesystem writes.

Example output:

```text
Wrote file: internal/example/doc.go
<diagnostics-status ok="true" message="build succeeded">
$ go build -o /dev/null ./internal/example
</diagnostics-status>
<lint-status ok="true" message="no issues found" mode="fix">
$ gofmt -l -w internal/example
</lint-status>
```

## Behavior

- The agent supplies one file path and the complete content to write.
- Relative paths are resolved from the sandbox dir.
- If the file does not exist, the tool creates it.
- If the file already exists, the tool replaces its full contents.
- Parent directories are created when needed.
- On success, the filesystem reflects the supplied content exactly.
- Failed writes are not presented as applied changes.
