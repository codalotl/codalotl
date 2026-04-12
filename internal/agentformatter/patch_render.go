package agentformatter

import (
	"strings"
	"unicode"

	"github.com/codalotl/codalotl/internal/llmstream"
)

type patchChangeKind int

const (
	patchChangeAdd patchChangeKind = iota + 1
	patchChangeDelete
	patchChangeEdit
	patchChangeRenameOnly
)

type patchLineKind int

const (
	patchLineContext patchLineKind = iota + 1
	patchLineAdd
	patchLineRemove
	patchLineGap
	patchLineSummary
)

type patchChange struct {
	kind       patchChangeKind
	path       string
	toPath     string
	replaceAll bool // replaceAll indicates that an edit tool call requested global replacement.
	lines      []patchLine
}

type patchLine struct {
	kind   patchLineKind
	prefix string
	text   string
}

func applyPatchHeaderSegments(change patchChange) []textSegment {
	switch change.kind {
	case patchChangeAdd:
		return []textSegment{
			{text: "Add", style: runeStyle{color: colorColorful, bold: true}},
			{text: " " + change.path},
		}
	case patchChangeDelete:
		return []textSegment{
			{text: "Delete", style: runeStyle{color: colorColorful, bold: true}},
			{text: " " + change.path},
		}
	case patchChangeRenameOnly:
		return []textSegment{
			{text: "Rename", style: runeStyle{color: colorColorful, bold: true}},
			{text: " " + change.path},
			{text: " → " + change.toPath, style: runeStyle{color: colorAccent}},
		}
	default:
		segments := []textSegment{
			{text: "Edit", style: runeStyle{color: colorColorful, bold: true}},
			{text: " " + change.path},
		}
		if change.toPath != "" {
			segments = append(segments, textSegment{text: " → " + change.toPath, style: runeStyle{color: colorAccent}})
		}
		if change.replaceAll {
			segments = append(segments, textSegment{text: " (replace all)", style: runeStyle{color: colorAccent}})
		}
		return segments
	}
}

func patchChangeFromPresentedDiffEdit(edit llmstream.DiffEdit) patchChange {
	change := patchChange{
		path:       sanitizeText(firstNonEmpty(edit.OldPath, edit.NewPath)),
		toPath:     sanitizeText(edit.NewPath),
		replaceAll: edit.ReplaceAll,
		lines:      patchLinesFromPresentedDiff(edit.Lines),
	}

	switch edit.Kind {
	case llmstream.DiffEditKindAdd:
		change.kind = patchChangeAdd
		change.path = sanitizeText(firstNonEmpty(edit.NewPath, edit.OldPath))
		change.toPath = ""
	case llmstream.DiffEditKindDelete:
		change.kind = patchChangeDelete
		change.path = sanitizeText(firstNonEmpty(edit.OldPath, edit.NewPath))
		change.toPath = ""
	case llmstream.DiffEditKindRename:
		change.path = sanitizeText(firstNonEmpty(edit.OldPath, edit.NewPath))
		change.toPath = sanitizeText(firstNonEmpty(edit.NewPath, edit.OldPath))
		if len(change.lines) == 0 {
			change.kind = patchChangeRenameOnly
			change.toPath = sanitizeText(edit.NewPath)
		} else {
			change.kind = patchChangeEdit
		}
	default:
		change.kind = patchChangeEdit
		if edit.NewPath != "" && edit.NewPath != edit.OldPath {
			change.toPath = sanitizeText(edit.NewPath)
		} else {
			change.toPath = ""
		}
	}

	return change
}

func patchChangeFromPresentedDiffSummary(diff llmstream.Diff) patchChange {
	if len(diff.Edits) == 0 {
		return patchChange{}
	}
	return patchChangeFromPresentedDiffEdit(diff.Edits[0])
}

func patchLinesFromPresentedDiff(lines []llmstream.DiffLine) []patchLine {
	patchLines := make([]patchLine, 0, len(lines))
	for _, line := range lines {
		switch line.Kind {
		case llmstream.DiffLineKindAdd:
			patchLines = append(patchLines, patchLine{
				kind:   patchLineAdd,
				prefix: "+ ",
				text:   line.Text,
			})
		case llmstream.DiffLineKindDelete:
			patchLines = append(patchLines, patchLine{
				kind:   patchLineRemove,
				prefix: "- ",
				text:   line.Text,
			})
		case llmstream.DiffLineKindOmitted:
			text := line.Text
			if strings.TrimSpace(text) == "" {
				text = "⋮"
			}
			patchLines = append(patchLines, patchLine{
				kind: patchLineGap,
				text: text,
			})
		default:
			patchLines = append(patchLines, patchLine{
				kind:   patchLineContext,
				prefix: " ",
				text:   line.Text,
			})
		}
	}

	return cleanPatchLines(patchLines)
}

func (f *textTUIFormatter) appendPatchLinesTUI(builder *strings.Builder, width int, lines []patchLine) {
	for _, line := range lines {
		builder.WriteByte('\n')
		firstPrefix := "     "
		restPrefix := "       "
		if line.kind == patchLineGap || line.kind == patchLineSummary {
			restPrefix = firstPrefix
		}
		runes := f.buildPatchStyledRunes(line)
		builder.WriteString(f.wrapStyledText(runes, width, firstPrefix, restPrefix))
	}
}

func (f *textTUIFormatter) cliPatchLines(lines []patchLine) []string {
	if len(lines) == 0 {
		return nil
	}
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		var runes []styledRune
		runes = append(runes, f.buildStyledRunes("     ", runeStyle{}, nil)...)
		runes = append(runes, f.buildPatchStyledRunes(line)...)
		result = append(result, f.styledString(runes))
	}
	return result
}

func (f *textTUIFormatter) buildPatchStyledRunes(line patchLine) []styledRune {
	base := runeStyle{color: colorNormal}
	switch line.kind {
	case patchLineAdd:
		base.color = colorGreen
	case patchLineRemove:
		base.color = colorRed
	case patchLineGap, patchLineSummary:
		base.color = colorAccent
	}
	sanitized := sanitizeText(line.text)
	text := line.prefix + sanitized
	runes := f.buildStyledRunes(text, base, nil)
	if line.kind != patchLineGap && line.kind != patchLineSummary {
		accentLineNumbers(runes)
	}
	return runes
}

func accentLineNumbers(runes []styledRune) {
	for i := 0; i < len(runes); i++ {
		r := runes[i].r
		if r == ' ' {
			runes[i].style.color = colorAccent
			continue
		}
		if unicode.IsDigit(r) {
			runes[i].style.color = colorAccent
			continue
		}
		break
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func cleanPatchLines(lines []patchLine) []patchLine {
	if len(lines) == 0 {
		return lines
	}
	result := make([]patchLine, 0, len(lines))
	for _, line := range lines {
		if line.kind == patchLineGap {
			if len(result) > 0 && result[len(result)-1].kind == patchLineGap {
				continue
			}
		}
		result = append(result, line)
	}
	for len(result) > 0 && result[len(result)-1].kind == patchLineGap {
		result = result[:len(result)-1]
	}
	for len(result) > 0 && result[0].kind == patchLineGap {
		result = result[1:]
	}
	return result
}
