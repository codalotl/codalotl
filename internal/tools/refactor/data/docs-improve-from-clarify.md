Potentially improve public Go documentation for package `{{PACKAGE_REL_DIR}}` (`{{PACKAGE_IMPORT_PATH}}`).

The following Q/A records were obtained via `clarify_public_api`, a tool used by package-mode agents to gather information about another package.

Use these Q/A records as signals of public API confusion. Improve documentation wherever a senior Go engineer would naturally put the information: package docs, related type docs, function docs, or another public documentation location in this package. Do not blindly paste answers into the originally questioned identifier's doc comment.

Make documentation-only public-doc improvements. Do not change behavior, APIs, tests, generated files.

Deciding not to make edits is completely acceptable, and preferrable to making weird or bad edits:
- If the existing documentation already resolves these questions naturally, make no edits.
- If a senior Go engineer would reject these edits as weird or out-of-place, make no edits.
- If the answer appears to be wrong or misleading, make no edits.
- If the Q/A records are valid, but the information is already implied by existing Go documentation, make no edits.

Remember: some of these questions were asked by middling, cheap LLMs. Do not over-index on them. Only improve docs if the resultant docs pass muster.

## Clarify Q/A records

{{CLARIFY_QA_RECORDS}}
