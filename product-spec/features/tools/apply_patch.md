# `apply_patch`

`apply_patch` edits files by applying a structured patch. This is the exact same structure as OpenAI's Codex uses (in fact, we ported their golden tests). With a single `apply_patch` call:
- multiple hunks of one file can be edited.
- multiple files can be edited.
- files can be created and deleted.
- (all of the above simultaneously in a single call)

In addition to editing files, diagnostics and lints are run afterwards (and results of which are added to the `apply_patch` output). This only applies in package mode.

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

Errors include invalid patch text, missing paths, conflicts, denied permissions, and failed filesystem writes.

Example output:

```text
<apply-patch ok="true">
Updated the following files:
M internal/gocas/doc.go
</apply-patch>
<diagnostics-status ok="true" message="build succeeded">
$ go build -o /dev/null ./internal/gocas
</diagnostics-status>
<lint-status ok="true">
<command ok="true" message="no issues found" mode="fix">
$ gofmt -l -w internal/gocas
</command>
<command ok="true" message="no issues found" mode="fix">
$ codalotl spec fmt internal/gocas
</command>
<command ok="true" message="no issues found" mode="fix">
$ codalotl docs reflow internal/gocas
</command>
<command ok="true" message="no issues found" mode="check">
$ staticcheck ./internal/gocas
</command>
</lint-status>
```

## Behavior

- The agent submits one patch containing one or more file operations.
- File operations can add files, update files, delete files, and move files.
- Patch paths are sandbox-relative.
- The patch grammar uses file-oriented sections rather than line-numbered diffs.
- The tool applies the patch atomically enough that failed parses, authorization failures, and conflicts are not presented as applied edits.
- The post-edit checks are only run in package mode.
- The diagnostic check runs when editing Go code (it tries to compile the package).
- Any configured lints are run. Some of these lints can re-edit the file - their CLI output is included in the tool output, so the LLM knows the file was edited.

Example lint status where `gofmt` fixed the formatting:

```text
<apply-patch ok="true">
Updated the following files:
M internal/gocas/gocas.go
</apply-patch>
<diagnostics-status ok="true" message="build succeeded">
$ go build -o /dev/null ./internal/gocas
</diagnostics-status>
<lint-status ok="true">
<command ok="true" mode="fix">
$ gofmt -l -w internal/gocas
internal/gocas/gocas.go
</command>
</lint-status>
```

## Presentation

Example display:

```text
• Edit internal/gocas/doc.go
     - // Package gocas stores Go package and code-unit metadata in content-addressable storage.
     + // Package gocas stores metadata for Go packages and their default code units in content-addressable storage.
     ⋮
     - // The package selects CAS roots from CODALOTL_CAS_DB or from .codalotl/cas under the nearest git root. It also provides package
         recertification and pruning helpers
     - // for maintaining Go-aware CAS records.
     + // CAS roots come from CODALOTL_CAS_DB when set, or from .codalotl/cas under the nearest git root. The package also provides recertification
         and pruning helpers
     + // for maintaining Go-aware CAS records.
      package gocas
```

## Permissions

Writes are authorized before the patch is applied.

In package mode, `apply_patch` reinforces the selected package boundary and then runs package-aware checks after successful edits.
