# `read_file`

`read_file` reads one text file.

## Inputs

- `path`: file path, absolute or sandbox-relative.
- `line_numbers`: optional boolean; when true, returned lines are prefixed with 1-based line numbers.
- `request_permission`: optional boolean; asks the user for approval when the target is outside the current automatic authorization boundary.

## Output

The tool returns the readable file content with enough metadata for the agent to understand what was read and whether the output was truncated.

Errors include invalid parameters, missing files, directory paths, unreadable files, denied permissions, and unsupported binary-style content.

Example output:

```text
<file name="some.go" line-count="5" byte-count="123" any-line-truncated="false" file-truncated="false">
package foo

func f() int {
    return 0
}
</file>
```

## Behavior

- The agent supplies one file path.
- Relative paths are resolved from the sandbox dir.
- The path must resolve to an existing file.
- The tool returns text contents, not a binary payload.
- Very large files, very long files, very long lines, invalid UTF-8, or otherwise unsuitable file contents may be truncated.
- Truncation is visible in the result rather than silently changing meaning.
- The agent can ask for line numbers when it needs stable references. Ordinary reads do not include line numbers.

## Presentation

Example display:

```text
• Read path/to/file.go
```

## Permissions

Reads are authorized before opening the file.

In package mode, `read_file` reinforces the selected package boundary: the agent can directly read files in the selected package code unit, while outside reads require explicit authorization or context supplied through other Go-aware tools.
