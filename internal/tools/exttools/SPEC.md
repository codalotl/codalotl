# exttools

Presentation specs for tooling helpers.

## Presentations

### diagnostics

- In progress: `Run Diagnostics some/path`
- Complete: `Ran Diagnostics some/path`
- Failed runs own their error display.

### fix_lints

- In progress: `Fix Lints some/path`
- Complete: `Fixed Lints some/path`
- Body: summarized output, up to 5 visible lines, with wrapper tags omitted.

### run_tests

- In progress: `Run Tests some/path`
- Complete: `Ran Tests some/path`
- Prefer concise body when test/lint status sections are available: `Tests: pass|fail|unknown | Lints: pass|fail|unknown`
- Otherwise body is summarized output, up to 5 visible lines.

### run_project_tests

- In progress: `Run Tests ./...`
- Complete: `Ran Tests ./...`
- Success body: `Passed`
- Failure body: `Failed:` plus failing package/test lines.
