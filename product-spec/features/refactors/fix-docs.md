# Fix Docs

Codalotl allows users to find and fix documentation errors in their codebase:
- Documentation is just godoc-style comments on top-level identifiers (not internal function comments) - see `features/docs.md`.
- Documentation errors are detected per-package. Running it on a package finds/fixes errors in the non-test code, the test code, and any `_test` blackbox package.
- Each found documentation error is automatically fixed.
- It is incredibly important that this does NOT triggle false positives. If users find it annoying, it's useless.
    - Documentation errors are ONLY saying something that is materially false.
    - It does NOT flag omissions.
    - It permits imprecise language.
    - Lack of documentation is not an error (identifiers without docs aren't scanned).
- One conceptual test: fixing docs should be idempotent - the second run should not find any errors.
- After running on a package, it writes a CAS file, hashed against the fixed code, indicating the package has been processed.
    - The CAS file should be written via manual CLI invocation, `codalotl_cli`, and `refactor`.

## CLI

CLI commands:
- `codalotl docs fix path/to/pkg`: find and fix all doc errors in given package.
- `codalotl docs fix --identifiers="foo,bar" path/to/pkg`: limit identifiers checked to foo and bar.

### Example CLI Output

The following examples are illustrative - exact output here is not prescriptive (but is allowed).

Example 1: no issues found

```text
% codalotl docs fix internal/codeunit
Finding documentation errors in codeunit (non-tests)...
> Finding issues in NewCodeUnit, DefaultGoCodeUnit, *CodeUnit.Name, *CodeUnit.Includes, *CodeUnit.IncludedFiles, *CodeUnit.IncludeEntireSubtree, *CodeUnit.IncludeDir, *CodeUnit.IncludeSubtreeUnlessContains, *CodeUnit.PruneEmptyDirs, *CodeUnit.PruneStructuralDirs, CodeUnit
< found no issue in *CodeUnit.PruneEmptyDirs.
< found no issue in DefaultGoCodeUnit.
< found no issue in *CodeUnit.Includes.
< found no issue in *CodeUnit.IncludeDir.
< found no issue in *CodeUnit.Name.
< found no issue in NewCodeUnit.
< found no issue in *CodeUnit.IncludeSubtreeUnlessContains.
< found no issue in CodeUnit.
< found no issue in *CodeUnit.IncludeEntireSubtree.
< found no issue in *CodeUnit.IncludedFiles.
< found no issue in *CodeUnit.PruneStructuralDirs.
Found no documentation issues
Applied 0 documentation fix(es).
```

Example 2: several issues found (NOTE: this example has elided sections)

```text
...
< found no issue in *expectedSnippet.MissingDocs.
< found issue in *expectedSnippet.PublicSnippet:
  Issue: PublicSnippet can return the preserveMixedPublicSnippet field when preserveMixed is true and hasPreserveMixedPublicSnippet is true, rather than always returning the publicSnippet field.
< found no issue in assertFuncSnippet.
...
< found no issue in dedent.
< found no issue in expectedValue.
> Requesting docs for 1 identifiers: Snippet (11.7k toks)
< Got 1 snippets
> Requesting docs for 2 identifiers: Module, Package (28.5k toks)
< Got 2 snippets
> Requesting docs for 1 identifiers: groupFunctionsByType (3.5k toks)
< Got 1 snippets
> Requesting docs for 1 identifiers: ValueSnippet (9.8k toks)
< Got 1 snippets
> Requesting docs for 1 identifiers: extractUnattachedComments (2.6k toks)
< Got 1 snippets
> Requesting docs for 1 identifiers: filterInterface (2.3k toks)
< Got 1 snippets
> Requesting docs for 1 identifiers: getEmbeddedFieldName (1.1k toks)
< Got 1 snippets
> Requesting docs for 1 identifiers: filterExportedTypes (2.9k toks)
< Got 1 snippets
> Requesting docs for 1 identifiers: *expectedSnippet.PublicSnippet (238 toks)
< Got 1 snippets
> Requesting docs for 1 identifiers: FuncSnippet (12.4k toks)
< Got 1 snippets
> Requesting docs for 1 identifiers: *Module.ReadPackage (2.5k toks)
< Got 1 snippets
> Requesting docs for 1 identifiers: *Package.MarshalDocumentation (1.6k toks)
< Got 1 snippets
> Requesting docs for 1 identifiers: PackageDocSnippet (8.3k toks)
< Got 1 snippets
Applied 14 documentation fix(es).
- filterExportedTypes
- filterInterface
- groupFunctionsByType
- *Module.ReadPackage
- Module
- *Package.MarshalDocumentation
- Package
- Snippet
- FuncSnippet
- PackageDocSnippet
- *expectedSnippet.PublicSnippet
- getEmbeddedFieldName
- ValueSnippet
- extractUnattachedComments
```

## Orchestrator

- This CLI command is available in the `codalotl_cli` tool.
- It is also available in the refactor tool, as `docs-fix`.
- When the orchestrator calls via either, the tokens used are added to the agents total, as displayed in the TUI.
- The generic agent and orchestrator can easily be instructed to run this. Ex: `run refactor(docs-fix, internal/some/pkg)`.
    - The orchestrator knows to commit the CAS entries.

### TUI Output

The following example is illustrative - exact output here is not prescriptive (but is allowed).

```text
• Refactoring docs-fix in internal/diff
  • Finding documentation errors in diff (non-tests)...
    > Finding issues in DiffText, buildDiffLines, splitPreserveEOL, trimEOL, diffsToSpans, Diff.RenderPretty, Diff.RenderUnifiedDiff, Diff.validate,
    Op, Diff, DiffHunk, DiffLine, DiffSpan
  • < found no issue in buildDiffLines.
  • < found no issue in trimEOL.
  • < found no issue in diffsToSpans.
  • < found no issue in DiffSpan.
  • < found no issue in Diff.
  • < found no issue in DiffText.
  • < found no issue in splitPreserveEOL.
  • < found no issue in DiffHunk.
  • < found no issue in Diff.RenderUnifiedDiff.
  • < found no issue in Op.
  • < found no issue in Diff.validate.
  • < found issue in DiffLine:
      Issue: The comment says a line lacks a trailing \n only when the input text to DiffText had no \n, but the final line can lack a trailing
      newline even when the input contains earlier newlines.
  • < found no issue in Diff.RenderPretty.
  • > Requesting docs for 1 identifiers: DiffLine (4.9k toks)
  • < Got 1 snippets
  • Applied 1 documentation fix(es).
• Refactored docs-fix in internal/diff
  └ Successfully applied refactor
```
