# `fix_lints`

`fix_lints` applies configured lint fixes to a Go package.

## Inputs

- `path`: package directory path, absolute or sandbox-relative.

## Output

The tool returns the lint pipeline result for the requested path, including which checks ran and whether issues were fixed or remain.

In fix mode, a successful result means all enabled lint steps either found no issues or fixed the issues they are able to fix. A failure may mean a command failed, or that a check-only lint found issues that cannot be fixed automatically.

Errors include invalid parameters, missing paths, non-directory paths, denied permissions, command failures, and unfixable lint issues.

Example output:

```text
<lint-status ok="false">
<command ok="true" message="no issues found" mode="fix">
$ gofmt -l -w catalog
</command>
<command ok="true" message="no issues found" mode="fix">
$ codalotl spec fmt catalog
</command>
<command ok="false" mode="check">
$ codalotl spec diff catalog
DIFF 1/1
type: impl-missing
ids: *Catalog.Count
spec: SPEC.md:29
impl: <missing>
</command>
</lint-status>
```

## Behavior

- The agent supplies one package directory path.
- Relative paths are resolved from the sandbox dir.
- The path must resolve to an existing directory.
- The tool runs the configured lint pipeline in the dedicated lint-fix situation.
- Lint steps that support fixing may edit files.
- Steps that only support checking may still report remaining issues.
- If no lint steps are configured, the tool reports a successful no-linters status.

## Presentation

Example display:

```text
• Fixed Lints internal/example
```

When there is useful lint output, the presentation may include a compact summary:

```text
• Fixed Lints internal/example
  └ $ gofmt -w internal/example
    internal/example/foo.go
```

The presentation should summarize output rather than dump the full structured lint status.

## Permissions

Writes are authorized before lint fixes run.

In package mode, `fix_lints` gives the agent a package-aware cleanup tool that can apply configured mechanical fixes while preserving the selected package boundary.
