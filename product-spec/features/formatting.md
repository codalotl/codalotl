# Formatting

Codalotl formats agent events for people in the TUI and in noninteractive human-readable output. It also exposes noninteractive JSON output for programs.

This feature describes the human-facing shape of event formatting. Exact color, wrapping, and truncation details are not prescriptive unless called out explicitly.

## Surfaces

### TUI

The TUI renders events into the messages area as a chat-like stream. It uses bullets, indentation, semantic color, and width-aware wrapping so the user can scan progress without reading raw event payloads.

The TUI should keep meaning visible without relying on color. Success, failure, warnings, errors, and permission decisions must remain understandable in plain or low-color terminals.

Subagent activity is attributed by indentation or grouping. Deeper agents are visually nested under the work that created them.

### Noninteractive Human Output

`codalotl exec` without `--json` renders the same semantic event stream as terminal text on stdout. It should look similar to the TUI but is optimized for ordinary terminal output rather than an alt-screen chat UI.

Fast tool calls should not create noisy duplicate lines. If a tool starts and completes quickly, noninteractive output may show only the completed presentation. If a tool runs long enough, its in-progress presentation appears first, followed by its completion.

For example, a quick read might only show:

```text
• Read internal/agent/event.go
```

A slower command might show both progress and completion:

```text
• Running go test ./internal/agent
• Ran go test ./internal/agent
  └ ok   github.com/codalotl/codalotl/internal/agent
```

### Noninteractive JSON

`codalotl exec --json` is not a formatted human transcript. It emits newline-delimited JSON events with stable structured fields.

Human formatting rules do not change JSON event identity, ordering, or fields. Tool presentations like `Read internal/agent/event.go` are display decisions; JSON exposes the underlying tool call and result.

Example:

```json
{"type":"tool_call","agent":{"id":"root","depth":0},"tool":{"call_id":"call_1","name":"read_file","type":"function_call","input":"{\"path\":\"internal/agent/event.go\"}"}}
{"type":"tool_complete","agent":{"id":"root","depth":0},"tool":{"call_id":"call_1","name":"read_file","type":"function_call"},"result":{"output":"package agent\n...","is_error":false}}
```

## Spec Formatting Notation

Human-readable output uses real ANSI escape codes in the TUI and noninteractive terminal output. ANSI escape codes are difficult for humans to read in a markdown spec, so examples in this document use XML-like semantic tags instead.

These tags communicate intended styling:

- `<accent>`: lower-emphasis terminal accent, used for bullets, connectors, and secondary output.
- `<colorful>`: high-emphasis action color, used for tool verbs and active work.
- `<success>`: success color, used for successful completions and added diff lines.
- `<error>`: error color, used for failures and deleted diff lines.
- `<bold>`: bold text.
- `<em>`: emphasized text.

Untagged text is ordinary terminal text.

The tags are not literal output. For example, the spec may write:

```text
<accent>•</accent> <bold><colorful>Read</colorful></bold> internal/agent/event.go
```

In a real terminal, Codalotl emits a bullet, the word `Read` in bold action color, and the path in ordinary text, using ANSI codes when color is enabled. With `--no-color` or a plain theme, the same line remains understandable without color:

```text
• Read internal/agent/event.go
```

When an example needs to specify styling, show the plain version first and the annotated version second. The plain version communicates layout and text; the annotated version communicates styling.

## Event Classes

Most visible events fall into these classes:

- User messages: queued or sent user-authored text.
- Assistant text: assistant prose shown as normal chat output.
- Assistant reasoning: reasoning summaries when available, shown as lower-emphasis assistant output.
- Tool calls: an in-progress tool presentation.
- Tool completions: the completed tool presentation, with success or failure state.
- Tool output: display-only output emitted while a tool is running.
- Subagent starts and completions: nested agent work, optionally grouped under a label.
- Status events: warnings, retries, cancellation, errors, and final completion.
- Permissions: approval decisions or prompts, depending on the surface.

The formatter should sanitize terminal-hostile text, preserve useful line breaks, and wrap prose to the current display width when the surface supports it.

## Tool Presentation Framework

Each tool can provide a semantic presenter. A presenter describes the tool with:

- A summary line, such as `Read internal/foo/foo.go`.
- Optional body content, such as output lines, a checklist, a paragraph, or a diff.
- A completion behavior: replace or append.
- Optional status or error-handling details.

The final TUI or noninteractive formatter adds bullets, indentation, wrapping, and color.

### Replace Presentations

Replace presentations are for quick or atomic tools. The in-progress line and completed line represent the same action, so the completed line replaces or suppresses the earlier one when practical.

Typical replace tools:

- `read_file`
- `ls`
- `edit`
- `write`
- `apply_patch`
- `update_plan`
- `shell`
- `skill_shell`
- `run_tests`
- `diagnostics`
- `fix_lints`

### Append Presentations

Append presentations are for long-lived tools or tools whose start and finish are separately meaningful. The start remains visible and the completion is appended.

Typical append tools:

- `clarify_public_api`
- `check_spec_conformance`
- `refactor`
- Other tools that launch labeled subagents or multi-step work.

### Fallback Presentations

If a tool has no semantic presenter, the human output falls back to a generic tool line using the tool name and input. Completion may include summarized raw result output.

Example:

```text
• Tool custom_tool {"target":"internal/foo"}
• Tool custom_tool {"target":"internal/foo"}
  └ result line
```

## Core Tool Examples

The following examples are illustrative and representative. Real output may wrap differently, include ANSI color, omit fast in-progress lines in noninteractive output, or use a narrower result summary.

### `read_file`

`read_file` is a replace presentation with a compact summary. The result body is usually not shown because the file content is for the agent, not for the user transcript.

Plain:

```text
• Read internal/agent/event.go
```

Annotated:

```text
<accent>•</accent> <bold><colorful>Read</colorful></bold> internal/agent/event.go
```

If the read fails, the completion line uses failure styling and includes the error:

Plain:

```text
• Read internal/missing.go
  └ Error: path does not exist
```

Annotated:

```text
<error>•</error> <bold><colorful>Read</colorful></bold> internal/missing.go
  <accent>└</accent> <error>Error: path does not exist</error>
```

### `ls`

`ls` is also a replace presentation. It shows the listed path, not the raw XML or structured result.

Plain:

```text
• List internal/tools
```

Annotated:

```text
<accent>•</accent> <bold><colorful>List</colorful></bold> internal/tools
```

### `update_plan`

`update_plan` is a replace presentation with a checklist body. It is meant to show work state, not raw JSON parameters.

Plain:

```text
• Update Plan
  └ Inspect existing product specs
    ✔ Read summary and related docs
    □ Draft formatting feature spec
    □ Verify style and save file
```

Annotated:

```text
<accent>•</accent> <bold><colorful>Update Plan</colorful></bold>
  <accent>└</accent> <accent>Inspect existing product specs</accent>
    <accent>✔ Read summary and related docs</accent>
    <bold><colorful>□ Draft formatting feature spec</colorful></bold>
    <accent>□ Verify style and save file</accent>
```

### `apply_patch`

`apply_patch` is a replace presentation. When the patch is parseable, the formatter presents a semantic diff rather than the raw patch envelope.

Plain:

```text
• Edit internal/foo/foo.go
     - old line
     + new line
```

Annotated:

```text
<accent>•</accent> <bold><colorful>Edit</colorful></bold> internal/foo/foo.go
     <error>- old line</error>
     <success>+ new line</success>
```

A multi-file patch can produce multiple file-level sections:

Plain:

```text
• Add internal/foo/new.go
     + package foo
• Delete internal/foo/old.go
• Edit internal/foo/foo.go → internal/foo/bar.go
     - old name
     + new name
```

Annotated:

```text
<accent>•</accent> <bold><colorful>Add</colorful></bold> internal/foo/new.go
     <success>+ package foo</success>
<accent>•</accent> <bold><colorful>Delete</colorful></bold> internal/foo/old.go
<accent>•</accent> <bold><colorful>Edit</colorful></bold> internal/foo/foo.go → internal/foo/bar.go
     <error>- old name</error>
     <success>+ new name</success>
```

`edit` and `write` use the same semantic diff family as `apply_patch`.

### `shell` and `skill_shell`

`shell` and `skill_shell` show the command as the user-visible action. Completed results summarize command output rather than dumping unlimited stdout and stderr.

Plain:

```text
• Running go test ./...
```

Annotated:

```text
<accent>•</accent> <bold><colorful>Running</colorful></bold> go test ./...
```

On completion:

Plain:

```text
• Ran go test ./...
  └ ok   github.com/codalotl/codalotl/internal/agent
    ok   github.com/codalotl/codalotl/internal/tools/coretools
    … +8 lines
```

Annotated:

```text
<success>•</success> <bold><colorful>Ran</colorful></bold> go test ./...
  <accent>└</accent> <accent>ok   github.com/codalotl/codalotl/internal/agent</accent>
    <accent>ok   github.com/codalotl/codalotl/internal/tools/coretools</accent>
    <accent>… +8 lines</accent>
```

Failures use failure styling and show the most relevant output lines:

Plain:

```text
• Ran go test ./internal/foo
  └ Error: exit status 1
```

Annotated:

```text
<error>•</error> <bold><colorful>Ran</colorful></bold> go test ./internal/foo
  <accent>└</accent> <error>Error: exit status 1</error>
```

### `run_tests`

`run_tests` is a Go-aware test presentation. It summarizes test and lint state instead of exposing XML-ish tool output.

Plain:

```text
• Ran Tests ./internal/tools/toolsets
  └ Tests: pass | Lints: pass
```

Annotated:

```text
<success>•</success> <bold><colorful>Ran Tests</colorful></bold> ./internal/tools/toolsets
  <accent>└</accent> <accent>Tests: pass | Lints: pass</accent>
```

If lints fail:

Plain:

```text
• Ran Tests ./internal/tools/toolsets
  └ Tests: pass | Lints: fail
```

Annotated:

```text
<error>•</error> <bold><colorful>Ran Tests</colorful></bold> ./internal/tools/toolsets
  <accent>└</accent> <accent>Tests: pass | Lints: fail</accent>
```

### `get_public_api`

`get_public_api` is a package-aware read presentation. It shows the target package and, when identifiers were requested, the identifier list as body output.

Plain:

```text
• Read Public API internal/gocode
  └ Package, Module, Snippet
```

Annotated:

```text
<accent>•</accent> <bold><colorful>Read Public API</colorful></bold> internal/gocode
  <accent>└</accent> <accent>Package, Module, Snippet</accent>
```

### `clarify_public_api`

`clarify_public_api` is append-style because it can launch a subagent and the question and answer are both useful progress. It hides or replaces raw subagent final text when a presenter owns the final answer.

Plain:

```text
• Clarifying API Package in internal/gocode
  └ How does Package represent black-box test packages?
• Clarified API Package in internal/gocode
  └ Package represents the parsed Go package and includes test-package distinctions in its snippets.
```

Annotated:

```text
<accent>•</accent> <bold><colorful>Clarifying API</colorful></bold> Package <accent>in</accent> internal/gocode
  <accent>└</accent> <accent>How does Package represent black-box test packages?</accent>
<success>•</success> <bold><colorful>Clarified API</colorful></bold> Package <accent>in</accent> internal/gocode
  <accent>└</accent> <accent>Package represents the parsed Go package and includes test-package distinctions in its snippets.</accent>
```

### `check_spec_conformance`

`check_spec_conformance` is append-style and may include compact per-package status instead of raw subagent JSON.

Plain:

```text
• Checking SPEC conformance
  • internal/gocode: started
  • internal/gocode: conforms
• Checked SPEC conformance
  └ 1 package conforms
```

Annotated:

```text
<accent>•</accent> <bold><colorful>Checking SPEC conformance</colorful></bold>
  <accent>•</accent> internal/gocode: started
  <success>•</success> internal/gocode: conforms
<success>•</success> <bold><colorful>Checked SPEC conformance</colorful></bold>
  <accent>└</accent> <accent>1 package conforms</accent>
```

## Guidelines for New Tools

When adding a new tool, design its presentation around what the user needs to understand while the agent works. The display should answer: what is happening, where it is happening, whether it succeeded, and what detail is worth scanning.

### Summary Line

The summary line should start with an action phrase. The action phrase is bold and colorful. Targets, paths, identifiers, commands, and package names are usually normal text.

Good summary shapes:

Plain:

```text
• Read internal/foo/foo.go
• Run Diagnostics ./internal/foo
• Clarifying API Foo in internal/foo
```

Annotated:

```text
<accent>•</accent> <bold><colorful>Read</colorful></bold> internal/foo/foo.go
<accent>•</accent> <bold><colorful>Run Diagnostics</colorful></bold> ./internal/foo
<accent>•</accent> <bold><colorful>Clarifying API</colorful></bold> Foo <accent>in</accent> internal/foo
```

### Color and Emphasis

Use color as a scanning aid, not as the only source of meaning.

- Bullets use accent for in-progress calls, success for successful completions, and error for failed completions.
- The main action phrase is bold and colorful.
- Connector words like `in`, body connectors like `└`, ordinary output, omitted-line notices, and secondary detail usually use accent.
- Error text uses error styling and should also include explicit words like `Error:` or `failed`.
- Success styling should not replace success text when the outcome is otherwise ambiguous.
- Plain/no-color output must still read correctly.

### Body Content

Only include body content that helps the user track work.

- Use a diff body for edits, writes, patches, moves, adds, and deletes.
- Use output lines for command output, tool logs, and compact raw results.
- Use a checklist for plans or multi-step status.
- Use a paragraph for short prose answers or summaries.
- Summarize long output and show an omitted-line count rather than dumping everything.
- Do not show raw XML, JSON, or provider payloads when a concise semantic summary is possible.

Body lines are indented under the summary. The first body line commonly uses `└`; later body lines align under it. Diff lines use deeper indentation and color added lines as success and deleted lines as error.

### Replace vs Append

Use replace presentation when the call and result are the same user-visible unit of work. This is right for atomic operations like reading a file, listing a directory, updating a plan, applying a patch, or running a single command.

Use append presentation when the start and finish are separately meaningful. This is right for long-running workflows, tools that launch subagents, or tools where the in-progress question/request and the final answer/status are both useful.

Fast replace tools may only show the completed line in noninteractive human output. Append tools should leave both start and completion visible.

### Errors

A failed tool should make failure visible at the line where the user expects the result.

Plain:

```text
• Read internal/missing.go
  └ Error: path does not exist
```

Annotated:

```text
<error>•</error> <bold><colorful>Read</colorful></bold> internal/missing.go
  <accent>└</accent> <error>Error: path does not exist</error>
```

If a tool can produce partial semantic output, keep the useful body and attach the error near the failed item. For example, a patch failure can still show the best-effort file diff, with an error line attached to the relevant edit.

### Subagents

Tools that launch subagents should avoid exposing raw descendant chatter when a compact grouped presentation is clearer. A labeled subagent should show a labeled start and terminal line, with deeper activity grouped or hidden when appropriate.

When a tool owns the final subagent answer, it should present that answer as the tool result instead of making the user read raw JSON or an implementation-specific final assistant message.
