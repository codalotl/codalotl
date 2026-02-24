## Fixing SPEC diff failures
When `codalotl spec diff` fails, the package's public API does not match the public API described by `SPEC.md`.
- See $spec-md for general guidance on `SPEC.md` files.
- Conformance:
    - `SPEC.md` is a minimum: implementation may be a superset (extra fields/methods/block elements are OK).
    - If `SPEC.md` has docs for a decl, impl docs must match exactly (including placement). If `SPEC.md` has no docs, impl may.
    - Ordering matters; function bodies ignored.
- Picking a side:
    - If your task intentionally changed Go code (API/signature/docs), update `SPEC.md` to match.
    - If your task intentionally changed `SPEC.md` (or you're implementing new API from spec), update Go code to match.
    - If you're not sure, pick the docs that are better, erring on more specific.
    - Be very wary of deleting information from `SPEC.md`.
- For non-docs related diffs (ex: code mismatch; missing impl), if it's pre-existing and unrelated to your task, do NOT fix (avoid large, unrelated changes).
