// Package diff computes and renders text diffs between an "old" and a "new" string.
//
// Representation: A Diff holds the complete OldText/NewText and an ordered slice of hunks that, when concatenated, reconstruct both sides. Each hunk has an Op:
//   - OpEqual: unchanged region (OldText == NewText)
//   - OpInsert: text present only in the new side (OldText == "")
//   - OpDelete: text present only in the old side (NewText == "")
//   - OpReplace: text changed on both sides
//
// For non-equal hunks, Lines holds per-line changes; for non-equal lines, Spans holds intra-line segments. Lines generally include the trailing '\n' if it was present
// in the input; Spans never contain '\n'.
//
// Invariants:
//   - concat(hunks.OldText) == Diff.OldText
//   - concat(hunks.NewText) == Diff.NewText
//   - If hunk.Op == OpEqual, hunk.Lines is nil; otherwise, concatenating the line texts equals the hunk text.
//   - If line.Op == OpEqual, line.Spans is nil; otherwise, concatenating the span texts equals the line text (allowing for an optional trailing '\n').
//
// Granularity: The exact grouping of changes into hunks/lines/spans is a policy choice of DiffText and may evolve. Consumers should rely on the invariants above
// rather than any particular chunking strategy.
//
// Getting a diff: Use DiffText to compute a Diff:
//
//	d := diff.DiffText(oldText, newText)
//	fmt.Println(d.RenderUnifiedDiff(false, "old.txt", "new.txt", 3))
//
// Rendering: For human consumption:
//   - Diff.RenderPretty emits a simplified, colorized view (no @@ hunk headers) with "+"/"-"/" " line markers and highlighted intra-line additions/deletions. Filenames
//     are optional; pass "" to omit headers. contextSize controls how many unchanged lines are shown around changes.
//   - Diff.RenderUnifiedDiff emits a unified diff. Set color to true to include ANSI colors.
//
// Newlines: This package treats '\n' as the line separator. The last line may not end with '\n'; that fact is preserved in Lines. Spans never include '\n'.
package diff
