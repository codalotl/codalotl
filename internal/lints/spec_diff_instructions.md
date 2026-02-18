## Fixing SPEC diff failures
When `codalotl spec diff` fails, the public API of the package does not match the public API described by the SPEC.md.
- See $spec-md for guidance in SPEC.md files.
- In all cases, use judgement for which version is correct. Don't clobber user changes if they just changed the SPEC.md (ex: refined a comment; changed behavior of API; etc).
- Fix whitespace and documentation divergence.
- If there's an "id mismatch" (ex: SPEC.md wants a var block, but impl has individual var decls), reorganize decls to match.
- If there's a missing implementation, or mismatched code:
    - If it's unrelated to your task, DO NOT FIX (we don't want a large, unrelated diff, with possibly complex changes).
    - Otherwise, fix.
