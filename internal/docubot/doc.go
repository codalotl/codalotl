// Package docubot provides LLM-assisted tools to add, improve, polish, and validate Go documentation. It operates on a gocode.Package, applies changes to source files, and returns
// documentation-only diffs.
//
// Use AddDocs to create missing doc comments (with token-budgeted requests, optional inclusion of test files, and exclusions for generated code). Existing comments are not overwritten.
// Detect a request that would exceed the budget with errors.Is(err, ErrTokenBudgetExceeded). When repeated attempts cannot make progress, errors.Is(err, ErrTriesExceeded) matches.
//
// Use FindAndFixDocErrors to scan existing comments on functions, methods, and types for material issues and apply fixes. You can scope the scan to specific identifiers. The function
// may return partial results with a non-nil error; check both the returned changes and error.
//
// Use ImproveDocs to generate alternatives for existing comments, choose the better version, and apply only the winners. When HideCurrentDocs is set, the model does not see current
// comments (to encourage first-principles rewrites).
//
// Use Polish for minimal, style-focused rewording of existing comments. It preserves code and spacing and aims for small, idempotent edits.
//
// Documenting test files (including a black-box _test package) is supported. Test entry points (ex: TestXxx/BenchmarkXxx) are not documented. Some operations may partially update files
// before returning an error; inspect both the diff and the error to decide how to proceed.
package docubot
