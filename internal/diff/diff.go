package diff

// Op is an operation from old text to new text.
type Op int

// Operations from old text to new text.
const (
	OpEqual Op = iota
	OpInsert
	OpDelete
	OpReplace
)

// Diff is a diff from old text to new text.
//
// As an illustration: imagine a code file is edited: two separate functions are edited in the middle of the file. This will produce:
//   - Hunks[0] will be OpEqual (the prefix of the file).
//   - Hunks[1] will contain the first change: a group of contiguous lines that were changed. OpReplace.
//   - Hunks[2] will be OpEqual (the lines between the edits).
//   - Hunks[3] will contain the second change. Imagine some code was strictly inserted. OpInsert.
//   - Hunks[last] will be OpEqual (the suffix of the file).
//
// It is a policy question of how granular Hunks are (in theory, the above illustrative example could be achieved by a single Hunk with OpReplace). See DiffText for policies.
//
// Invariants:
//   - concat(Hunks.OldText) == OldText
//   - concat(Hunks.NewText) == NewText
type Diff struct {
	OldText string     // Entire original text.
	NewText string     // Entire revised text.
	Hunks   []DiffHunk // Ordered hunks that cover the whole diff and reconstruct OldText/NewText.
}

// DiffHunk represents a contiguous group of lines. The \n character is part of the hunk and line (ex: if a hunk is in the middle of some text is removed, OldText for that hunk would
// be \n terminated).
//
// Operations:
//   - OpEqual: OldText == NewText
//   - OpInsert: OldText=="" && NewText!=""
//   - OpDelete: OldText!="" && NewText==""
//   - OpReplace: OldText != "" and NewText != ""
//
// Invariants:
//   - If OpEqual, Lines is nil. Otherwise,
//   - concat(Lines.OldText) == OldText
//   - concat(Lines.NewText) == NewText
type DiffHunk struct {
	Op      Op         // Operation for this hunk (OpEqual, OpInsert, OpDelete, or OpReplace).
	OldText string     // Concatenation of old lines in this hunk; empty for inserts.
	NewText string     // Concatenation of new lines in this hunk; empty for deletes.
	Lines   []DiffLine // Per-line diffs when Op != OpEqual; nil when OpEqual.
}

// DiffLine is a diff on a single line. Each line usually ends with (and includes) \n, unless the input text to DiffText had no \n.
//
// Operations follow the pattern of DiffHunk.
//
// Invariants:
//   - If OpEqual, Spans is nil. Otherwise,
//   - concat(Spans.OldText) + \n? == OldText (\n? is an optional newline, since spans cannot contain \n, but lines usually do)
//   - concat(Spans.NewText) + \n? == NewText
type DiffLine struct {
	Op      Op         // Operation for this line (OpEqual, OpInsert, OpDelete, or OpReplace).
	OldText string     // Entire old line (including trailing newline if present); empty for inserts.
	NewText string     // Entire new line (including trailing newline if present); empty for deletes.
	Spans   []DiffSpan // Intra-line segments when Op != OpEqual; nil when OpEqual. Spans never contain newlines.
}

// DiffSpan is a diff within a line. It MUST NOT contain any \n.
//
// Operations follow the pattern of DiffHunk.
//
// DiffSpan is designed to be flexible in terms of policies for what constitutes a good span: it supports anything from single-character diffs (ex: adding a letter to a word), to per-word
// diffs, to per-word-group diffs. These policies are determined by DiffText.
type DiffSpan struct {
	Op      Op     // Operation performed by this span (OpEqual, OpInsert, OpDelete, or OpReplace).
	OldText string // Substring from the old line; empty for inserts.
	NewText string // Substring from the new line; empty for deletes.
}

// defaultEOL is the EOL ('\n').
//
// This constant exists because the design may change to allow configurable EOLs (maybe Windows needs "\r\n"), and this provides a nice hook to find callsites.
const defaultEOL = "\n"
