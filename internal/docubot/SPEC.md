# docubot

Docubot offers functions to add documentation to Go packages, improve existing documentation, and find errors in documentation. Documentation here means Go doc-style comments; intra-function comments are out of scope.

## Dependencies

- `internal/gocode` owns the Go parsing, snippets, and identifier handling (`type Identifiers` in this package is allowed).
- `internal/updatedocs` owns applying doc edits to source code and reflowing doc comments. Reflowing means controlling placement (EOL vs `Doc`) and changing comment width.
- `internal/specmd` owns reading and preparing SPEC.md context for LLM prompts.
- `internal/agent` owns agent tool-run LLM usage accounting.
- NOTE: `docubot`, by extension, should NOT directly be doing these things.

## BaseOptions

`BaseOptions` carries shared settings for LLM-backed operations, including model / completer selection, health / logger context, and optional writer for user-facing progress output.

- User-facing progress/status text goes to the configured writer, or stdout if nil.
    - Also written to logs, so that logs have context about what is happening.
- Debug/digansotic/error logs use logger / health context.
- In agent tool-run contexts, successful LLM completions report token usage for cost accounting.

## LLM Context

LLM-backed package documentation operations include the package's `SPEC.md`, if present, with `## Public API` removed.

## Definitions and Mechanics

Definitions:
- A declaration is a package-level `func`, `type`, `var`, or `const` clause in a file (an `*ast.FuncDecl` or `*ast.GenDecl` whose parent is the file node).
- A spec is an element inside a `GenDecl` that does the real work of defining something: `ValueSpec` and `TypeSpec` for vars/consts and types, respectively.
- An identifier is any named symbol introduced by a declaration or spec, plus the identifiers that name struct fields and interface methods, plus the special package identifier.
- An identifier is exported/public if it starts with a capital letter. Otherwise, it is unexported/private.
    - Methods are exported iff their receiver is exported and the method name is exported.
- A package-level identifier is any identifier defined by a declaration or a spec, but does NOT include field identifiers. Includes overall package documentation.
- A field identifier is any field or method in a struct or interface.

Mechanics:
- Generally, when a primary method like `AddDocs` accepts a package, it means that package as well as a black-box `_test` package, if present.
- Overall package documentation counts as a piece of public documentation (comment above `package`, preferably in a `doc.go` file) and has an identifier (see `gocode.PackageIdentifier`). Only for main packages (not _test packages).
- `init` functions are not documentable (but don't count against us as undocumented). They have identifiers like `init:file.go:15:6`.
- Anonymous identifiers (ex: `var _ Foo`; `func _()`) are not documentable (but don't count against us as undocumented). They have identifiers like `_:file.go:15:5`.
- The prompts generally recommend documenting the block as well as the specs. But if each spec is documented, documenting the block itself is optional.
- For consts in a block: if the block is documented, each containing const is considered documented.
- Comments inside functions are out of scope for this package, including for specs and functions defined inside functions.
- Structs (e.g., `type foo struct { ... }`) are fully documented if the overall type is commented and each field is commented, including nested anonymous structs.
    - These nested anonymous structs can be basic (ex: `type foo struct { bar struct { baz int } }`), as well as pointers thereof, slices thereof, etc.
- Interfaces are fully documented if the overall type is documented, and each method is documented.
- Embedded structs/interfaces need documentation on that field. Ex: `type foo struct { otherpkg.Bar }` where `otherpkg.Bar` is some other struct.
- Do not edit generated files.
- Do not clobber special comments like `//go:embed` directives.

- TODO: describe how "floater" comments work.

## AddDocs

The `AddDocs` function adds missing documentation to a package by directly editing the package's files.
- By default, it documents all identifiers, excluding test files. It does not document generated identifiers, anonymous identifiers, or init identifiers.
- It never removes, fixes, or edits an existing comment (other than reflowing).

Options include:
- `DocumentTestFiles`: if true, we also document test files, including black-box tests (package somepkg_test). Does not document TestXxx/BenchmarkXxx/etc functions.
- `OnlyDocumentExportedIdentifiers`: if true, we only document exported identifiers.
- `OnlyDocumentImportantIdentifiers`: if true, we only document important identifiers.
- `ExcludeIdentifiers`: any identifiers here are not documented. Notes:
    - Excluding a type also excludes all of the type's fields (for structs). Same for interfaces and methods.
    - If an excluded identifier is part of a multi-identifier spec/field (ex: `var Foo, Bar int`), and at least one of the identifiers is not excluded, a comment may still be added.

Notes:
- If `OnlyDocumentExportedIdentifiers`, source like `var Public, private = 0, 1` may document both `Public` and `private` (but must at least ensure `Public` has docs). The snippet may never be split into multiple decls.
    - In "ensuring `Public` has docs": the decl must have a comment (its contents are not considered, and may not actually mention `Public`).
    - This also applies to things like a struct's fields with mixed public/private. Ex: `type Foo struct { Public, private int }`.
- If `OnlyDocumentExportedIdentifiers`, private structs' public fields are NOT documented.
- If `OnlyDocumentExportedIdentifiers`, public structs' private fields are NOT documented (unless they share a spec with a public field, as per above).
- If `OnlyDocumentExportedIdentifiers`, similar rules apply to interfaces. Private interfaces' methods are not documented, and public interfaces' private methods are also not documented.
- `OnlyDocumentExportedIdentifiers` and `DocumentTestFiles` should combine as expected: documents main package exported identifiers, and exported identifiers in the test package(s), but not the TestXxx/etc ones.
- `OnlyDocumentImportantIdentifiers` and `OnlyDocumentExportedIdentifiers` are mutually exclusive selection modes.
- Important identifiers are package docs, exported identifiers, all types and their fields/methods, functions/methods with at least 20 source lines, and identifiers in cyclic groups with fan-in >= 10 or fan-out >= 12.
- Important fan-in and fan-out are based on `gocodecontext.IdentifierGroup` after cyclic grouping. A group's edge counts apply to every identifier selected from that group.
- If all important identifiers are already documented, `OnlyDocumentImportantIdentifiers` returns without making LLM requests.
- `OnlyDocumentImportantIdentifiers` and `DocumentTestFiles` should combine as expected: documents important identifiers in main package and test package(s), but not TestXxx/etc functions.

## Clarification Improvements

Clarification improvements use `clarify_public_api` Q/A pairs as doc-comment guidance.

- Each clarification targets an identifier in the package.
- Only answers useful for public docs are reflected.
- Existing docs are left unchanged when clarification is irrelevant, already covered, or too narrow.
- Documentation edits are minimal and doc-comment-only.

## Public API

```go
// Clarification is one clarify_public_api answer to consider for documentation.
type Clarification struct {
	Identifier string
	Question   string
	Answer     string
}

// ImproveFromClarificationsOptions configures clarification-driven documentation improvements.
type ImproveFromClarificationsOptions struct {
	BaseOptions
}

// ImproveFromClarifications improves docs when clarification answers add useful public-doc context.
func ImproveFromClarifications(pkg *gocode.Package, clarifications []Clarification, options ImproveFromClarificationsOptions) ([]IncorporatedFeedback, error)
```
