# docubot

Docubot offers functions to add documentation to Go packages, improve existing documentation, find errors in documentation, etc. Documentation here is just Go doc-style comments.

## Dependencies

- `internal/gocode` owns the Go parsing, snippets, and identifier handling (`type Identifiers` in this package is allowed).
- `internal/updatedocs` owns applying doc edits to source code and reflowing doc comments. Reflowing means controlling placement (eol vs Doc), and changes comment width.
- NOTE: `docubot`, by extension, should NOT directly be doing these things.

## Definitions and Mechanics

Definitions:
- A declaration is a package-level `func`, `type`, `var`, or `const` clause in a file (an `*ast.FuncDecl` or `*ast.GenDecl` whose parent is the file node).
- A spec is the element(s) that appears inside a `GenDecl` and does the real work of defining something: `ValueSpec` and `TypeSpec` for vars/consts and types, respectively.
- An identifier is any named symbol introduced by a declaration or spec, plus the identifiers that name struct fields and interface methods.
- An identifier is exported/public if it starts with a Capital letter. Otherwise, it is unexported/private.
    - funcs with receivers are exported iff their receiver is exported AND the method name is exported.
- A package-level identifier is any identifier defined by a declaration or a spec, but does NOT include field identifiers. Includes overall package documentation.
- A field identifier is any field or method in a struct or interface.

Mechanics:
- Overall package documentation counts as a piece of public documentation (comment above `package`, preferrably in `doc.go` file).
- `init` functions are not documentable (but don't count against us as undocumented). They have identifiers like `init:file.go:15:6`.
- Anonymous identifiers (ex: `var _ Foo`; `func _()`) are not documentable (but don't count against us as undocumented). They have identifiers like `_:file.go:15:5`.
- For consts in a block: if the block is documented, each containing const is considered documented.
- The prompts generally recommend documenting the block as well as the specs. But if each spec is documented, documenting the block itself is optional.

## AddDocs

The `AddDocs` function adds missing documentation to a package by directly editing the package's files.
- By default, it documents all identifiers, excluding test files. It does not document generated identifiers.
- It never removes, fixes, or edits an existing comment (other than reflowing).
- For structs, documents the struct type itself, and recursively documents the fields. `OnlyDocumentExportedIdentifiers` below applies recursively to nested structs and their fields.

Options include:
- `DocumentTestFiles`: if true, we also document test files, including black-box tests (package somepkg_test). Does not document TestXxx/BenchmarkXxx/etc functions.
- `OnlyDocumentExportedIdentifiers`: if true, we only document exported identifiers.
- `ExcludeIdentifiers`: any identifiers here are not documented.

Notes:
- If `OnlyDocumentExportedIdentifiers`, source like `var Public, private = 0, 1` may document both `Public` and `private` (but must at least ensure `Public` has docs). The snippet may never be split into multiple decls.
    - In "ensuring `Public` has docs": the decl must have a comment (its contents are not considered, and may not actually mention `Public`).
    - This also applies to things like a struct's fields with mixed public/private. Ex: `type Foo struct { Public, private int }`.
- If `OnlyDocumentExportedIdentifiers`, private structs' public fields are NOT documented.
- If `OnlyDocumentExportedIdentifiers`, public structs' private fields are NOT documented.
- If `OnlyDocumentExportedIdentifiers`, similar rules applies to interfaces. Private interfaces's methods are not documented, and public interface's private methods are also not documented.
- `OnlyDocumentExportedIdentifiers` and `DocumentTestFiles` should combine as spected: documents main package public identifiers, and exported (capitalized) identifiers in the test package(s), but not the TestXxx/etc ones.
