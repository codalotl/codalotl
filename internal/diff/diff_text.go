package diff

import (
	"fmt"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// DiffText diffs oldText to newText, returning a Diff.
func DiffText(oldText, newText string) Diff {

	var lineDiffs []diffmatchpatch.Diff
	dmp := diffmatchpatch.New()
	var decode func(string) []string

	// Diff based on lines:
	rOld, rNew, lineArray := dmp.DiffLinesToRunes(oldText, newText)
	lineDiffs = dmp.DiffMainRunes(rOld, rNew, false)
	lineDiffs = dmp.DiffCleanupMerge(lineDiffs)

	// Decode rune-string back to slice of original lines using the lineArray mapping.
	decode = func(s string) []string {
		if s == "" {
			return nil
		}
		out := make([]string, 0, len(s))
		for _, r := range s {
			idx := int(r)
			if idx >= 0 && idx < len(lineArray) {
				out = append(out, lineArray[idx])
			}
		}
		return out
	}

	var hunks []DiffHunk
	var dels []string
	var ins []string

	flush := func() {
		if len(dels) == 0 && len(ins) == 0 {
			return
		}
		oldBlock := strings.Join(dels, "")
		newBlock := strings.Join(ins, "")
		var op Op
		switch {
		case len(dels) > 0 && len(ins) > 0:
			op = OpReplace
		case len(dels) > 0:
			op = OpDelete
		default:
			op = OpInsert
		}
		lines := buildDiffLines(dels, ins)
		hunks = append(hunks, DiffHunk{Op: op, OldText: oldBlock, NewText: newBlock, Lines: lines})
		dels = nil
		ins = nil
	}

	for _, d := range lineDiffs {
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			flush()
			eqLines := decode(d.Text)
			if len(eqLines) == 0 {
				continue
			}
			text := strings.Join(eqLines, "")
			hunks = append(hunks, DiffHunk{Op: OpEqual, OldText: text, NewText: text, Lines: nil})
		case diffmatchpatch.DiffDelete:
			dels = append(dels, decode(d.Text)...)
		case diffmatchpatch.DiffInsert:
			ins = append(ins, decode(d.Text)...)
		}
	}
	flush()

	diff := Diff{OldText: oldText, NewText: newText, Hunks: hunks}

	if err := diff.validate(); err != nil {
		panic(fmt.Errorf("DiffText: validate failed with %v", err))
	}

	return diff
}

// buildDiffLines constructs DiffLine entries and inline spans.
func buildDiffLines(deleteLines, insertLines []string) []DiffLine {
	// Pair up replacements for min(len(delete), len(insert)); leftovers are pure deletes/inserts.
	n := len(deleteLines)
	if len(insertLines) < n {
		n = len(insertLines)
	}
	var lines []DiffLine
	dmp := diffmatchpatch.New()

	for i := 0; i < n; i++ {
		oldLine := deleteLines[i]
		newLine := insertLines[i]
		oldCore, _ := trimEOL(oldLine, defaultEOL)
		newCore, _ := trimEOL(newLine, defaultEOL)
		if oldLine == newLine {
			lines = append(lines, DiffLine{Op: OpEqual, OldText: oldLine, NewText: newLine, Spans: nil})
			continue
		}
		spans := diffsToSpans(dmp.DiffMain(oldCore, newCore, false))
		lines = append(lines, DiffLine{Op: OpReplace, OldText: oldLine, NewText: newLine, Spans: spans})
	}
	for i := n; i < len(deleteLines); i++ {
		oldLine := deleteLines[i]
		oldCore, _ := trimEOL(oldLine, defaultEOL)
		var spans []DiffSpan
		if len(oldCore) > 0 {
			spans = []DiffSpan{{Op: OpDelete, OldText: oldCore, NewText: ""}}
		}
		lines = append(lines, DiffLine{Op: OpDelete, OldText: oldLine, NewText: "", Spans: spans})
	}
	for i := n; i < len(insertLines); i++ {
		newLine := insertLines[i]
		newCore, _ := trimEOL(newLine, defaultEOL)
		var spans []DiffSpan
		if len(newCore) > 0 {
			spans = []DiffSpan{{Op: OpInsert, OldText: "", NewText: newCore}}
		}
		lines = append(lines, DiffLine{Op: OpInsert, OldText: "", NewText: newLine, Spans: spans})
	}
	return lines
}

// splitPreserveEOL splits text by eol and preserves the eol on each line, except possibly the last.
func splitPreserveEOL(text, eol string) []string {
	if text == "" {
		return nil
	}
	if eol == "" {
		eol = defaultEOL
	}
	var lines []string
	for {
		idx := strings.Index(text, eol)
		if idx == -1 {
			if text != "" {
				lines = append(lines, text)
			}
			break
		}
		lines = append(lines, text[:idx+len(eol)])
		text = text[idx+len(eol):]
		if text == "" {
			break
		}
	}
	return lines
}

// trimEOL removes a trailing eol from a line if present.
func trimEOL(line, eol string) (string, bool) {
	if eol != "" && strings.HasSuffix(line, eol) {
		return line[:len(line)-len(eol)], true
	}
	return line, false
}

// diffsToSpans converts diffmatchpatch diffs to DiffSpan entries.
func diffsToSpans(diffs []diffmatchpatch.Diff) []DiffSpan {
	// Build initial spans, coalescing adjacent equals to reduce fragmentation:
	var spans []DiffSpan
	for _, d := range diffs {
		if d.Text == "" {
			continue
		}
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			if len(spans) > 0 && spans[len(spans)-1].Op == OpEqual {
				spans[len(spans)-1].OldText += d.Text
				spans[len(spans)-1].NewText += d.Text
				continue
			}
			spans = append(spans, DiffSpan{Op: OpEqual, OldText: d.Text, NewText: d.Text})
		case diffmatchpatch.DiffDelete:
			spans = append(spans, DiffSpan{Op: OpDelete, OldText: d.Text, NewText: ""})
		case diffmatchpatch.DiffInsert:
			spans = append(spans, DiffSpan{Op: OpInsert, OldText: "", NewText: d.Text})
		}
	}

	if len(spans) == 0 {
		return spans
	}

	// Iteratively collapse any non-equal run between equals into a single span:
	for {
		changed := false
		var normalized []DiffSpan
		for i := 0; i < len(spans); {
			s := spans[i]
			if s.Op == OpEqual {
				normalized = append(normalized, s)
				i++
				continue
			}
			// Collect a run of non-equal spans until next equal or end.
			j := i
			for j < len(spans) && spans[j].Op != OpEqual {
				j++
			}
			var oldBuf, newBuf strings.Builder
			for k := i; k < j; k++ {
				sk := spans[k]
				switch sk.Op {
				case OpDelete:
					oldBuf.WriteString(sk.OldText)
				case OpInsert:
					newBuf.WriteString(sk.NewText)
				case OpReplace:
					oldBuf.WriteString(sk.OldText)
					newBuf.WriteString(sk.NewText)
				}
			}
			var combinedOp Op
			switch {
			case oldBuf.Len() > 0 && newBuf.Len() > 0:
				combinedOp = OpReplace
			case oldBuf.Len() > 0:
				combinedOp = OpDelete
			case newBuf.Len() > 0:
				combinedOp = OpInsert
			default:
				// Nothing to add.
				i = j
				continue
			}
			normalized = append(normalized, DiffSpan{Op: combinedOp, OldText: oldBuf.String(), NewText: newBuf.String()})
			if j-i > 1 {
				changed = true
			}
			i = j
		}
		spans = normalized
		if !changed {
			break
		}
	}

	// Iteratively merge small equals sandwiched between non-equals:
	const maxSandwichedEqualLen = 8
	for {
		changed := false
		var normalized []DiffSpan
		// Helper to append while coalescing adjacent non-equals:
		appendWithCoalesce := func(s DiffSpan) {
			if len(normalized) > 0 && normalized[len(normalized)-1].Op != OpEqual && s.Op != OpEqual {
				prev := normalized[len(normalized)-1]
				var oldBuf, newBuf strings.Builder
				// Prev contribution:
				switch prev.Op {
				case OpDelete:
					oldBuf.WriteString(prev.OldText)
				case OpInsert:
					newBuf.WriteString(prev.NewText)
				case OpReplace:
					oldBuf.WriteString(prev.OldText)
					newBuf.WriteString(prev.NewText)
				}
				// Current contribution:
				switch s.Op {
				case OpDelete:
					oldBuf.WriteString(s.OldText)
				case OpInsert:
					newBuf.WriteString(s.NewText)
				case OpReplace:
					oldBuf.WriteString(s.OldText)
					newBuf.WriteString(s.NewText)
				}
				var combinedOp Op
				switch {
				case oldBuf.Len() > 0 && newBuf.Len() > 0:
					combinedOp = OpReplace
				case oldBuf.Len() > 0:
					combinedOp = OpDelete
				default:
					combinedOp = OpInsert
				}
				normalized[len(normalized)-1] = DiffSpan{Op: combinedOp, OldText: oldBuf.String(), NewText: newBuf.String()}
				return
			}
			normalized = append(normalized, s)
		}
		for i := 0; i < len(spans); {
			// If we find [non-eq][small eq][non-eq], merge the triplet.
			if i+2 < len(spans) && spans[i].Op != OpEqual && spans[i+1].Op == OpEqual && spans[i+2].Op != OpEqual && len(spans[i+1].OldText) <= maxSandwichedEqualLen {
				var oldBuf, newBuf strings.Builder
				// Left contribution:
				switch spans[i].Op {
				case OpDelete:
					oldBuf.WriteString(spans[i].OldText)
				case OpInsert:
					newBuf.WriteString(spans[i].NewText)
				case OpReplace:
					oldBuf.WriteString(spans[i].OldText)
					newBuf.WriteString(spans[i].NewText)
				}
				// Equal bridge contributes to both sides:
				oldBuf.WriteString(spans[i+1].OldText)
				newBuf.WriteString(spans[i+1].NewText)
				// Right contribution:
				switch spans[i+2].Op {
				case OpDelete:
					oldBuf.WriteString(spans[i+2].OldText)
				case OpInsert:
					newBuf.WriteString(spans[i+2].NewText)
				case OpReplace:
					oldBuf.WriteString(spans[i+2].OldText)
					newBuf.WriteString(spans[i+2].NewText)
				}
				var combinedOp Op
				switch {
				case oldBuf.Len() > 0 && newBuf.Len() > 0:
					combinedOp = OpReplace
				case oldBuf.Len() > 0:
					combinedOp = OpDelete
				default:
					combinedOp = OpInsert
				}
				appendWithCoalesce(DiffSpan{Op: combinedOp, OldText: oldBuf.String(), NewText: newBuf.String()})
				changed = true
				i += 3
				continue
			}
			// No triplet to merge; carry span over (coalescing adjacency if needed).
			appendWithCoalesce(spans[i])
			i++
		}
		spans = normalized
		if !changed {
			break
		}
	}
	return spans
}
