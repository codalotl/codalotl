# specmd

This package implements tools for processing SPEC.md files: parsing, formatting, and extracting code blocks.

SPEC.md files are currently assumed to be for Go packages only.

## Dependencies

- Markdown parsed with `github.com/yuin/goldmark`
- Go code parsed with `internal/gocode`

## Public API

```go {api}

// Spec represents a SPEC.md on disk.
type Spec struct {
    AbsPath string // absolute file path of SPEC.md
    Body string // Full contents of the file
}

// Read reads the path to create a Spec. If the path is not a "SPEC.md" file (case sensitive), an error is returned. The file is NOT parsed, nor verified to be markdown.
func Read(path string) (*Spec, error)

// Validate parses Body as a markdown file, and ensures each Go code block has valid code without syntax errors. The code is not checked for type errors.
// The first error encountered is returned; nil if no errors.
func (s *Spec) Validate() error

// GoCodeBlocks returns all multi-line Go code blocks in a ```go``` fence.
//   - These must be triple-backtick and multi-line, not inline `single-backtick` code spans.
//   - The fences MUST be tagged with `go`. Go code in triple-backtick fences without the Go tag are not included.
//
// If there are any problems parsing the markdown or if there are malformed code blocks (e.g. no closing triple-backticks), an error is returned. The Go code itself is not checked for errors.
func (s *Spec) GoCodeBlocks() ([]string, error)

// PublicAPIGoCodeBlocks returns those Go code blocks that are part of the public API of a package. This is determined by:
//   - If the code block has {api} in the info string.
//   - If the code block is in any headered section that includes "public api" (case-insensitive).
//   - If the code block is in any nested headered section of the above "public api". E.g., `## Public API\n### Types\n<code block>`.
//
// Errors are returned for the same reasons as GoCodeBlocks.
func (s *Spec) PublicAPIGoCodeBlocks() ([]string, error)


// FormatGoCodeBlocks runs each Go code block through the equivalent of `gofmt`, updating the file on disk and s.Body.
//
// If any Go code block has erroneous Go code (e.g. syntax error), it is ignored. The other Go code blocks are still formatted.
//
// It returns true if any modifications to the SPEC.md were made. An error is returned for file I/O issues or for invalid markdown. Go code with syntax errors do not cause errors.
func (s *Spec) FormatGoCodeBlocks() (bool, error)

type DiffType int

const (
    DiffTypeOther DiffType = iota // Unknown difference
    DiffTypeImplMissing // At least one snippet ID is missing in the implementation.
    DiffTypeIDMismatch // All IDs are present in the impl, but they span different snippets. E.g., one var block in SPEC, but impl has separate `var` decls.
    DiffTypeCodeMismatch // Both spec and impl have the same snippet, but code is different. E.g. diff function args; diff var values. Any docs are NOT considered. Whitespace is also not considered.
    DiffTypeDocMismatch // Docs between the two are mismatched.
    DiffTypeDocWhitespace // Whitespace is different (e.g. SPEC uses spaces but impl uses tabs).
)

// SpecDiff represents a difference from the SPEC.md and the actual implementation in .go files. Each diff corresponds to one `gocode.Snippet`. Note that one code block may contain
// multiple `gocode.Snippet` values, and one `gocode.Snippet` may contain multiple IDs. A correspondence between snippet and impl is made only by exact ID matches.
type SpecDiff struct {
    // The IDs of the snippet. Often this is just one string (e.g., a function name). It can be multiple IDs for things like var blocks.
    // These IDs will match a snippet in the SPEC.md exactly.
    IDs []string

    SpecSnippet string // The snippet in the SPEC. May be "" if missing.
    SpecLine int // The line number in the SPEC.

    ImplSnippet string // The snippet in the actual implementation. May be "" if missing.
    ImplFile string // The .go file containing the impl.
    ImplLine int // The line number.

    // DiffType represents the reason the specs differ. DiffTypeOther is a fallback if no pre-contemplated reason is discovered; otherwise, we prefer to return a lower iota value (e.g. DiffTypeCodeMismatch
    // is returned if there's both a DiffTypeCodeMismatch and a DiffTypeDocMismatch).
    DiffType DiffType
}

// ImplemenationDiffs finds differences between the public API declared in the SPEC.md and the actual public API in the corresponding Go package. It only checks those identifiers
// defined in the SPEC.md - if the public API is a strict superset, no differences are returned. If no differences are found, nil is returned.
//   - Only PublicAPIGoCodeBlocks are checked.
//   - If PublicAPIGoCodeBlocks contains method bodies, they are ignored (we're only checking the interface).
//   - That being said, variable declarations must match (and an anonymous function can be assigned to a variable - it is checked in this case).
func (s *Spec) ImplemenationDiffs() ([]SpecDiff, error)


// FormatDiffs formates and writes diffs to out, in a manner that would be helpful to a human or LLM in syncing up the spec and implementation.
func FormatDiffs(diffs []SpecDiff, out io.Writer) error
```
