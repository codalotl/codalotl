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
- `SubagentEventPolicy`: `HideFinalMessage`
- In-progress body: question text
- Complete body: answer text

### change_api

- Behavior: Append
- In progress: `Changing API in some/pkg`
- Complete: `Changed API in some/pkg`
- `SubagentEventPolicy`: `HideFinalMessage`
- In-progress body: instructions
- Complete body: result text

### update_usage

- Behavior: Append
- In progress: `Updating Usage in pkg/a, pkg/b, pkg/c (N more)`
- Complete: `Updated Usage in pkg/a, pkg/b, pkg/c (N more)`
- `SubagentEventPolicy`: `HideFinalMessage`
- In-progress body: instructions
- Complete body: result text
