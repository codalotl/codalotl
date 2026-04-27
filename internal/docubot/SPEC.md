# docubot

Docubot offers functions to add documentation to Go packages, improve existing documentation, find errors in documentation, etc. Documentation here is just Go doc-style comments.

## Dependencies

- `internal/gocode` owns the Go parsing, snippets, and identifier handling.
- `internal/updatedocs` owns applying doc edits to source code and reflowing doc comments. Reflowing means controlling placement (eol vs Doc), and changes comment width.
    - NOTE: `docubot`, but extension, should NOT directly be doing these things.

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
