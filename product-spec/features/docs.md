# Docs

Codalotl treats Go documentation as product surface: concise facts attached to Go identifiers, kept in sync with code and package intent.

## Feature Set

- `codalotl docs add`: adds missing Go doc comments.
- `codalotl docs fix`: finds existing docs that say materially false things, then fixes them.
- `codalotl docs reflow`: normalizes doc comment shape - width, blank lines, and EOL-vs-Doc placement. No semantic rewrite intended.
- `codalotl docs status`: prints package-by-package documentation status, similar to `codalotl spec status`, so users can see where missing docs, stale/unset `docs fix` certification, or reflow drift remain.
    - `docs add` status is computed from current code rather than CAS; `docs fix` status uses its package CAS.
- `docs-improve-from-clarify`: uses `clarify_public_api` answers to improve public docs.
    - When `clarify_public_api` is used, CAS entries are written, recording the question and answer.
    - This refactor workflow consumes them, updating the docs when it makes sense.

These tools are available via CLI commands, but also in various tools.
- `docs add` and `docs fix` are in the `refactor` and `codalotl_cli` tools.
- `docs reflow` is a lint that is auto-applied on edits.
- `docs-improve-from-clarify` can be called by agents like `orchestrator` via the `refactor` tool.

## Documentation Model

Documentation means Go doc-style comments attached to top-level identifiers:
- Package documentation (attached above `package foo`, usually in `doc.go`)
- Package-level funcs, types, vars, consts.
- Struct fields, including embedded fields.
- Interface methods and embeddings.
- Specs inside var/const/type blocks.

Comments inside function bodies are not documentation for this system.

An identifier is documented when the comment is attached to that identifier in the Go source model. Users experience this as "godoc would understand this comment belongs here", not merely "a nearby comment exists".

`Doc` comments and end-of-line comments can both document an identifier. The system may move between them:
- Functions, methods, package docs, type docs generally use `Doc`.
- Interface methods use `Doc`.
- Short field/spec docs may become end-of-line comments.
- Long field/spec docs usually become `Doc`.
- We sometimes chose `Doc` even if a field/spec's comment is short in order to imprive uniformity (ex: a list of 5 fields with a single EOL in the middle would look weird).

Package docs count as public documentation. They are represented as package identifier, usually a comment above `package`, preferably in `doc.go`.

For const blocks, a block comment counts as documenting each containing const. If each spec is documented, documenting the block is optional.

One comment may document multiple identifiers when source shape groups them together, such as `var Foo, Bar int` or `A, B string` fields. The docs system should not split declarations merely to make documentation more individualized.

Generated files are not edited. Special comments and directives, such as `//go:embed`, are not clobbered.

## Tests

By default, using `docs add` does not document test code. This can be documented using `--include-test`.

- Black-box `_test` packages are treated as the same target as their corresponding package. Ex: `docs add --include-test path/to/mypkg` will add docs to both `mypkg` and `mypkg_test`.
- Ordinary test functions (`TestXxx`, `BenchmarkXxx`, etc.) are not doc targets.

## Limitations

Anonymous identifiers and `init` functions are not documentable. Examples: `var _ Foo`, `func _()`, `func init()`. They do not count as missing docs. This is entirely due to coding architecture limitations, not desired product UX.

"Floaters" are top-level comments not attached as package, declaration, spec, or field docs. Our documentation system should preserve/reflow them, but:
- `docs add` does not add them. They also don't count as documenting any identifier. `docs add` should also not remove them.
- `docs fix` does not fix them.
