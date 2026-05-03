# Improve Public API Docs

An engineer working on another package read your public API docs (e.g., godocs-style comments on exported identifiers), but still had open questions about how to use your package. Their question, and the answer given back to this engineer by an agent reading the source code of this package, is included for you.

Your job is to decide whether that Q&A reveals useful public API documentation that belongs in this package. If so, edit the documentation of this package so future engineers have an easier time.

## Guidelines:
- Improve public docs only when the Q&A exposes reusable information future callers should have found in docs.
- It is acceptable to leave files unchanged. If no doc edit is useful, explain why in your final response.
- You MUST verify that the answer given to the question is accurate.
- To re-iterate: do NOT over-index on their question:
    - If their question was already clearly answerable by this package's docs, there's no need to update the docs.
    - Your goal is to produce timeless docs that are precise and concise, and appropriate for ANY calling package. You want to make the Go team proud. Do NOT over document.
- You can attach your documentation to exported identifiers or to overall package docs (typically a `doc.go` file with a comment above the `package` keyword).
- If the package has some missing documentation (or no documentation at all!), you may add some to the subset of public identifiers and package docs that are relevant to answer this question.
- If this package has a SPEC.md, read it.
- Do not change behavior, signatures, exported API shape, or production logic. Do not change tests except as needed for docs-adjacent checks.
- Some docs changes may need to be mirrored in SPEC.md's public API section(s). Use `fix_lints` to identify these situations.
- If you edit any files, use `fix_lints` and `run_tests`.

## General Documentation Writing Guidelines, Style, and Specific Mechanics

The following are guidelines for writing documentation **in general**. Use as appropriate.

### Writing Guidelines
- Follow Go's official documentation style.
- The test of good documentation is: 'can a user develop against this symbol with just the documentation, without looking at implementation details?'
- Good documentation will describe the *what*, and when it's not otherwise clear, the *why* (Foo does X. Call it when you want to ...)
- Good documentation will sometimes include: inputs, outputs, side effects, error conditions, example data, assumptions, preconditions, invariants, performance characteristics, and known issues.
- Good documentation hides implementation details and documents the API.
- _Given_ the documentation is good as per above, make the documentation concise and precise.

### Style
- Use 'ex' for parenthetical examples instead of 'ie', 'eg', or 'for instance' (ex: like this). But still use 'e.g.,' for parentheticals meaning 'in other words'.
- Prefer the ASCII character set unless the domain of the code calls for special characters (ex: use '-' instead of '—'; use `"` instead of '“' or '”').
- Doc comments (before an identifier on their own line) should be full sentences with capitalization and periods.
- End-of-line comments should also be full sentences with capitalization and periods.
- When documenting functions' input and output parameters, tend to NOT use a bulleted list when documenting input params UNLESS the number of inputs is 4 or more (and similarly for outputs).

### Specific Mechanics
- For structs and interfaces, also document fields and methods.
- For var/type blocks, you may document the block and individual specs.
- For const blocks, you may document the block, which is often sufficient to document all specs. ONLY IF the specs aren't self-describing enough, document the specs.
- Do NOT convert single-spec declarations into multi-spec blocks, or vice-versa.
- Choose _either_ a Doc comment (`// Foo ...`) _or_ an end-of-line comment (`... // Foo`) -- NEVER both for the same spec.
- Do NOT document 'sections' in lists of fields/values/etc. Sections will cause you to violate the above rule about either a doc or eol comment, since they count as a doc comment.

## Final response:
- If you edited docs, summarize what changed and what verification you ran.
- If you left files unchanged, say that clearly and give the reason.
