package diff

import (
	"fmt"
	"strings"
)

// RenderPretty returns a human-oriented, colorized rendering of d without unified-diff hunk headers. Each line is prefixed like a unified diff: " " for context,
// "-" for deletions, and "+" for insertions; replacements are shown as a "-" line followed by a "+" line. Within changed lines, intra-line additions and deletions
// are highlighted.
//
// If fromFilename and toFilename are both empty, no header is printed. Otherwise a single cyan header line is emitted in one of these forms:
//   - "add <to>:" when only toFilename is set
//   - "delete <from>:" when only fromFilename is set
//   - "<name>:" when both are equal
//   - "<from> -> <to>:" otherwise
//
// contextSize controls how many unchanged lines are shown before and after each group of changes. Two change groups separated by at most 2*contextSize unchanged
// lines are merged into a single group with the intervening lines shown as context.
//
// Lines are rendered without their trailing newline, and the returned string uses "\n" as the line separator. If there are no changes and no header is requested,
// the result is the empty string.
//
// The output contains ANSI 256-color escape sequences for line and span highlighting and is intended for terminals; it is not a machine-readable or standards-compliant
// diff. For a traditional unified diff, use RenderUnifiedDiff.
func (d Diff) RenderPretty(fromFilename string, toFilename string, contextSize int) string {
	// Colors (ANSI) for pretty output.
	const (
		reset     = "\x1b[0m"
		blackFG   = "\x1b[30m"
		pinkLine  = "\x1b[48;5;224m" // light pink for deleted lines
		pinkSpan  = "\x1b[48;5;217m" // slightly darker pink for deleted spans
		greenLine = "\x1b[48;5;194m" // light green for added lines
		greenSpan = "\x1b[48;5;114m" // slightly darker green for added spans
		cyanBold  = "\x1b[1;36m"
	)

	var out []string

	// Optional filename header in black (no background).
	if !(fromFilename == "" && toFilename == "") {
		header := ""
		switch {
		case fromFilename == "" && toFilename != "":
			header = fmt.Sprintf("add %s:", toFilename)
		case fromFilename != "" && toFilename == "":
			header = fmt.Sprintf("delete %s:", fromFilename)
		case fromFilename == toFilename:
			header = fmt.Sprintf("%s:", fromFilename)
		default:
			header = fmt.Sprintf("%s -> %s:", fromFilename, toFilename)
		}
		out = append(out, cyanBold+header+reset)
	}

	trim := func(s string) string {
		core, _ := trimEOL(s, defaultEOL)
		return core
	}

	// Render one DiffLine with inline span highlighting for either '-' (old) or '+' (new).
	renderLine := func(ln DiffLine, tag byte, baseBg string) string {
		if ln.Op == OpEqual {
			return trim(ln.OldText)
		}
		var b strings.Builder
		for _, sp := range ln.Spans {
			switch tag {
			case '-':
				switch sp.Op {
				case OpEqual:
					b.WriteString(sp.OldText)
				case OpDelete, OpReplace:
					// Emphasize deleted segments: darker pink background; reapply base after.
					b.WriteString(reset)
					b.WriteString(blackFG)
					b.WriteString(pinkSpan)
					b.WriteString(sp.OldText)
					b.WriteString(reset)
					b.WriteString(blackFG)
					b.WriteString(baseBg)
				case OpInsert:
					// Old side has nothing for inserts.
				}
			case '+':
				switch sp.Op {
				case OpEqual:
					b.WriteString(sp.NewText)
				case OpInsert, OpReplace:
					// Emphasize inserted segments: darker green background; reapply base after.
					b.WriteString(reset)
					b.WriteString(blackFG)
					b.WriteString(greenSpan)
					b.WriteString(sp.NewText)
					b.WriteString(reset)
					b.WriteString(blackFG)
					b.WriteString(baseBg)
				case OpDelete:
					// New side has nothing for deletes.
				}
			}
		}
		return b.String()
	}

	// Walk hunks, grouping nearby changes with context, but no @@ headers.
	i := 0
	for i < len(d.Hunks) {
		h := d.Hunks[i]
		if h.Op == OpEqual {
			i++
			continue
		}

		var lines []string

		// Pre-context from previous equal hunk tail.
		if i-1 >= 0 && d.Hunks[i-1].Op == OpEqual && contextSize > 0 {
			prevEqLines := splitPreserveEOL(d.Hunks[i-1].OldText, defaultEOL)
			k := contextSize
			if k > len(prevEqLines) {
				k = len(prevEqLines)
			}
			for _, ln := range prevEqLines[len(prevEqLines)-k:] {
				lines = append(lines, blackFG+" "+trim(ln)+reset)
			}
		}

		appendChange := func(hk DiffHunk) {
			for _, ln := range hk.Lines {
				switch ln.Op {
				case OpEqual:
					lines = append(lines, blackFG+" "+trim(ln.OldText)+reset)
				case OpDelete:
					content := renderLine(ln, '-', pinkLine)
					lines = append(lines, blackFG+pinkLine+"-"+content+reset)
				case OpInsert:
					content := renderLine(ln, '+', greenLine)
					lines = append(lines, blackFG+greenLine+"+"+content+reset)
				case OpReplace:
					contentDel := renderLine(ln, '-', pinkLine)
					lines = append(lines, blackFG+pinkLine+"-"+contentDel+reset)
					contentIns := renderLine(ln, '+', greenLine)
					lines = append(lines, blackFG+greenLine+"+"+contentIns+reset)
				}
			}
		}
		appendChange(h)

		// Possibly include bridging equals and subsequent changes if gap small enough.
		j := i + 1
		for j < len(d.Hunks) {
			if d.Hunks[j].Op != OpEqual {
				appendChange(d.Hunks[j])
				j++
				continue
			}
			eqLines := splitPreserveEOL(d.Hunks[j].OldText, defaultEOL)
			if j+1 < len(d.Hunks) && d.Hunks[j+1].Op != OpEqual && len(eqLines) <= 2*contextSize {
				for _, ln := range eqLines {
					lines = append(lines, " "+trim(ln))
				}
				j++
				appendChange(d.Hunks[j])
				j++
				continue
			}
			// Otherwise, include head context and stop this group.
			k := contextSize
			if k > len(eqLines) {
				k = len(eqLines)
			}
			for _, ln := range eqLines[:k] {
				lines = append(lines, " "+trim(ln))
			}
			break
		}

		// Advance to next unconsumed hunk index.
		i = j

		// Emit this group's lines.
		out = append(out, lines...)
	}

	return strings.Join(out, defaultEOL)
}

// RenderUnifiedDiff returns a unified diff. If color, the diff will include ANSI color markers.
func (d Diff) RenderUnifiedDiff(color bool, fromFilename string, toFilename string, contextSize int) string {
	// Colors (ANSI). Applied only if color==true.
	const (
		reset    = "\x1b[0m"
		red      = "\x1b[31m"
		green    = "\x1b[32m"
		magenta  = "\x1b[35m"
		cyanBold = "\x1b[1;36m"
	)

	colorize := func(s, code string) string {
		if !color {
			return s
		}
		return code + s + reset
	}

	// Helper to count lines in a block of text using the diff's EOL.
	countLines := func(text string) int {
		if text == "" {
			return 0
		}
		return len(splitPreserveEOL(text, defaultEOL))
	}

	type outLine struct {
		tag  byte   // ' ', '+', '-'
		text string // line content without EOL
	}

	var out []string

	// File headers
	out = append(out, colorize("--- "+fromFilename, cyanBold))
	out = append(out, colorize("+++ "+toFilename, cyanBold))

	// Current 1-based line numbers in old and new files at the start of the next hunk.
	oldPos := 1
	newPos := 1

	i := 0
	for i < len(d.Hunks) {
		h := d.Hunks[i]
		if h.Op == OpEqual {
			// Advance positions over equal hunks; no output.
			n := countLines(h.OldText)
			oldPos += n
			newPos += n
			i++
			continue
		}

		// Start assembling one unified hunk covering one or more change hunks and
		// possibly small equal separators between them.
		var lines []outLine

		// Pre-context from previous equal hunk tail.
		preK := 0
		if i-1 >= 0 && d.Hunks[i-1].Op == OpEqual && contextSize > 0 {
			prevEqLines := splitPreserveEOL(d.Hunks[i-1].OldText, defaultEOL)
			if contextSize < len(prevEqLines) {
				preK = contextSize
			} else {
				preK = len(prevEqLines)
			}
			for _, ln := range prevEqLines[len(prevEqLines)-preK:] {
				core, _ := trimEOL(ln, defaultEOL)
				lines = append(lines, outLine{tag: ' ', text: core})
			}
		}

		// Record starting line numbers for header.
		oldStart := oldPos - preK
		if oldStart < 1 {
			oldStart = 1
		}
		newStart := newPos - preK
		if newStart < 1 {
			newStart = 1
		}

		// Helper to append a change hunk's lines and advance positions.
		appendChange := func(hk DiffHunk) {
			// Produce per-line unified markers from hk.Lines.
			for _, ln := range hk.Lines {
				switch ln.Op {
				case OpEqual:
					core, _ := trimEOL(ln.OldText, defaultEOL)
					lines = append(lines, outLine{tag: ' ', text: core})
				case OpDelete:
					core, _ := trimEOL(ln.OldText, defaultEOL)
					lines = append(lines, outLine{tag: '-', text: core})
				case OpInsert:
					core, _ := trimEOL(ln.NewText, defaultEOL)
					lines = append(lines, outLine{tag: '+', text: core})
				case OpReplace:
					oldCore, _ := trimEOL(ln.OldText, defaultEOL)
					newCore, _ := trimEOL(ln.NewText, defaultEOL)
					lines = append(lines, outLine{tag: '-', text: oldCore})
					lines = append(lines, outLine{tag: '+', text: newCore})
				}
			}
			// Advance positions by full old/new line counts of this hunk.
			oldPos += countLines(hk.OldText)
			newPos += countLines(hk.NewText)
		}

		// Include the first change hunk at i.
		appendChange(h)

		// Possibly include bridging equals and subsequent change hunks if the
		// equal gap is small enough (<= 2*contextSize).
		j := i + 1
		for j < len(d.Hunks) {
			// If next is not equal, just append change and continue.
			if d.Hunks[j].Op != OpEqual {
				appendChange(d.Hunks[j])
				j++
				continue
			}
			// Next is equal. Decide whether to merge with following change.
			eqLines := splitPreserveEOL(d.Hunks[j].OldText, defaultEOL)
			if j+1 < len(d.Hunks) && d.Hunks[j+1].Op != OpEqual && len(eqLines) <= 2*contextSize {
				// Include entire equal as in-hunk context, then continue with next change.
				for _, ln := range eqLines {
					core, _ := trimEOL(ln, defaultEOL)
					lines = append(lines, outLine{tag: ' ', text: core})
				}
				oldPos += len(eqLines)
				newPos += len(eqLines)
				// Move to the change after this equal.
				j++
				appendChange(d.Hunks[j])
				j++
				continue
			}

			// Otherwise, include only post-context from the head of this equal and stop.
			postK := contextSize
			if postK > len(eqLines) {
				postK = len(eqLines)
			}
			for _, ln := range eqLines[:postK] {
				core, _ := trimEOL(ln, defaultEOL)
				lines = append(lines, outLine{tag: ' ', text: core})
			}
			oldPos += postK
			newPos += postK
			break
		}

		// Update i to continue after what we consumed. If we broke on an equal hunk (j points to it),
		// continue main loop from that equal hunk; otherwise from j.
		i = j

		// Compute header counts.
		oldCount := 0
		newCount := 0
		for _, ol := range lines {
			switch ol.tag {
			case ' ':
				oldCount++
				newCount++
			case '-':
				oldCount++
			case '+':
				newCount++
			}
		}

		// Emit hunk header and lines.
		header := fmt.Sprintf("@@ -%d,%d +%d,%d @@", oldStart, oldCount, newStart, newCount)
		out = append(out, colorize(header, magenta))
		for _, ol := range lines {
			line := string(ol.tag) + ol.text
			switch ol.tag {
			case '+':
				out = append(out, colorize(line, green))
			case '-':
				out = append(out, colorize(line, red))
			default:
				out = append(out, line)
			}
		}
	}

	return strings.Join(out, "\n")
}
