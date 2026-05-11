# pkgtools

Presentation specs for Go package tools.

## Presentations

### get_public_api

- Summary: `Read Public API some/pkg`
- Optional body: comma-separated identifiers from the call.

### get_usage

- Summary: `Read Usage some/pkg SomeIdentifier`
- Complete body: `Found N result.` or `Found N results.`

### module_info

- Summary: `Read Module Info`
- Optional body: call options like `Search: foo; Deps: true`.

### clarify_public_api

- Behavior: Append
- In progress: `Clarifying API SomeIdentifier in some/pkg`
- Complete: `Clarified API SomeIdentifier in some/pkg`
- Descendant subagent final message: hidden
- In-progress body: question text
- Complete body: answer text
- Clarification answer is produced by a read-only subagent.
- Do not edit target package source files.
- For sandbox packages, append a clarify CAS entry after a successful answer; include question, answer, origin package, target package, and identifier.
- Do not write package CAS outside the sandbox.

### change_api

- Behavior: Append
- In progress: `Changing API in some/pkg`
- Complete: `Changed API in some/pkg`
- Descendant subagent final message: hidden
- In-progress body: instructions
- Complete body: result text

### update_usage

- Behavior: Append
- In progress: `Updating Usage in pkg/a, pkg/b, pkg/c (N more)`
- Complete: `Updated Usage in pkg/a, pkg/b, pkg/c (N more)`
- Descendant subagent final message: hidden
- In-progress body: instructions
- Complete body: result text
