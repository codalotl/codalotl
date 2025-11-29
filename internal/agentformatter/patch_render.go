package agentformatter

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/codalotl/codalotl/internal/agent"
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
	kind   patchChangeKind
	path   string
	toPath string
	lines  []patchLine
}

type patchLine struct {
	kind   patchLineKind
	prefix string
	text   string
}

func (f *textTUIFormatter) tuiApplyPatchToolCall(e agent.Event, width int) string {
	changes, err := extractApplyPatchChanges(e.ToolCall)
	if err != nil || len(changes) == 0 {
		return f.tuiGenericToolCall(e, width)
	}
	var builder strings.Builder
	for idx, change := range changes {
		if idx > 0 {
			builder.WriteByte('\n')
		}
		f.renderApplyPatchChangeTUI(&builder, width, colorAccent, change, nil)
	}
	return builder.String()
}

func (f *textTUIFormatter) cliApplyPatchToolCall(e agent.Event) string {
	changes, err := extractApplyPatchChanges(e.ToolCall)
	if err != nil || len(changes) == 0 {
		return f.cliGenericToolCall(e)
	}
	var sections []string
	for _, change := range changes {
		sections = append(sections, f.renderApplyPatchChangeCLI(colorAccent, change, nil))
	}
	return strings.Join(sections, "\n")
}

func (f *textTUIFormatter) tuiApplyPatchToolComplete(e agent.Event, width int, success bool, _ string, output []toolOutputLine) string {
	changes, err := extractApplyPatchChanges(e.ToolCall)
	if err != nil || len(changes) == 0 {
		return f.tuiGenericToolComplete(e, width, success, "", output)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	var builder strings.Builder
	for idx, change := range changes {
		if idx > 0 {
			builder.WriteByte('\n')
		}
		var tail []toolOutputLine
		if !success && idx == len(changes)-1 {
			tail = output
		}
		f.renderApplyPatchChangeTUI(&builder, width, bullet, change, tail)
	}
	return builder.String()
}

func (f *textTUIFormatter) cliApplyPatchToolComplete(e agent.Event, success bool, _ string, output []toolOutputLine) string {
	changes, err := extractApplyPatchChanges(e.ToolCall)
	if err != nil || len(changes) == 0 {
		return f.cliGenericToolComplete(e, success, "", output)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	var sections []string
	for idx, change := range changes {
		var tail []toolOutputLine
		if !success && idx == len(changes)-1 {
			tail = output
		}
		sections = append(sections, f.renderApplyPatchChangeCLI(bullet, change, tail))
	}
	return strings.Join(sections, "\n")
}

func (f *textTUIFormatter) renderApplyPatchChangeTUI(builder *strings.Builder, width int, bullet colorRole, change patchChange, extra []toolOutputLine) {
	segments := applyPatchHeaderSegments(change)
	builder.WriteString(f.tuiBulletLine(width, bullet, segments...))
	if len(change.lines) > 0 {
		f.appendPatchLinesTUI(builder, width, change.lines)
	}
	if len(extra) > 0 {
		f.appendTUIToolOutput(builder, width, extra)
	}
}

func (f *textTUIFormatter) renderApplyPatchChangeCLI(bullet colorRole, change patchChange, extra []toolOutputLine) string {
	segments := applyPatchHeaderSegments(change)
	lines := []string{f.cliBulletLine(bullet, segments...)}
	if len(change.lines) > 0 {
		lines = append(lines, f.cliPatchLines(change.lines)...)
	}
	if len(extra) > 0 {
		lines = append(lines, f.cliToolOutputLines(extra)...)
	}
	return strings.Join(lines, "\n")
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
		return segments
	}
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

func extractApplyPatchChanges(call *llmstream.ToolCall) ([]patchChange, error) {
	source, ok := extractApplyPatchSource(call)
	if !ok {
		return nil, fmt.Errorf("no patch input")
	}
	return parseApplyPatch(source)
}

func extractApplyPatchSource(call *llmstream.ToolCall) (string, bool) {
	if call == nil {
		return "", false
	}
	input := strings.TrimSpace(call.Input)
	if input == "" {
		return "", false
	}

	var payload struct {
		Patch string `json:"patch"`
	}
	if err := json.Unmarshal([]byte(input), &payload); err == nil {
		if patch := strings.TrimSpace(payload.Patch); patch != "" {
			return patch, true
		}
	}
	return input, true
}

func parseApplyPatch(input string) ([]patchChange, error) {
	normalized := strings.ReplaceAll(input, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || strings.TrimSpace(lines[i]) != "*** Begin Patch" {
		return nil, fmt.Errorf("missing begin marker")
	}
	i++

	var changes []patchChange

	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			i++
			continue
		}
		if trimmed == "*** End Patch" {
			break
		}

		switch {
		case strings.HasPrefix(line, "*** Add File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: "))
			i++
			var patchLines []patchLine
			// Treat all following lines as additions until we hit another header.
			// Per grammar, add hunks are '+' lines; we also tolerate blank lines.
			newLine := 1
		forAdd:
			for i < len(lines) {
				cur := lines[i]
				if strings.HasPrefix(cur, "***") {
					break
				}
				switch {
				case cur == "":
					patchLines = append(patchLines, patchLine{kind: patchLineContext, prefix: " ", text: ""})
					newLine++
					i++
				case cur[0] == '+':
					patchLines = append(patchLines, patchLine{kind: patchLineAdd, prefix: "+", text: cur[1:]})
					newLine++
					i++
				default:
					// Stop if we encounter a non-add line (defensive).
					break forAdd
				}
			}
			patchLines = cleanPatchLines(patchLines)
			changes = append(changes, patchChange{
				kind:  patchChangeAdd,
				path:  path,
				lines: patchLines,
			})
		case strings.HasPrefix(line, "*** Delete File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File: "))
			changes = append(changes, patchChange{
				kind: patchChangeDelete,
				path: path,
			})
			i++
		case strings.HasPrefix(line, "*** Update File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Update File: "))
			i++
			moveTo := ""
			if i < len(lines) && strings.HasPrefix(lines[i], "*** Move to: ") {
				moveTo = strings.TrimSpace(strings.TrimPrefix(lines[i], "*** Move to: "))
				i++
			}
			var patchLines []patchLine
			// We hide hunk headers (lines starting with @@). Track them only to
			// insert a visual gap between hunks (⋮), but never at the very start.
			havePrintedLines := false
			for i < len(lines) {
				cur := lines[i]
				switch {
				case strings.HasPrefix(cur, "***"):
					goto finishUpdate
				case strings.HasPrefix(cur, "@@"):
					// Only add a gap if we already showed lines from a previous hunk.
					if havePrintedLines && (len(patchLines) == 0 || patchLines[len(patchLines)-1].kind != patchLineGap) {
						patchLines = append(patchLines, patchLine{kind: patchLineGap, text: "⋮"})
					}
					i++
				case strings.TrimSpace(cur) == "*** End of File":
					i++
				case cur == "":
					patchLines = append(patchLines, patchLine{kind: patchLineContext, prefix: " ", text: ""})
					havePrintedLines = true
					i++
				default:
					switch cur[0] {
					case '+':
						patchLines = append(patchLines, patchLine{kind: patchLineAdd, prefix: "+", text: cur[1:]})
					case '-':
						patchLines = append(patchLines, patchLine{kind: patchLineRemove, prefix: "-", text: cur[1:]})
					case ' ':
						patchLines = append(patchLines, patchLine{kind: patchLineContext, prefix: " ", text: cur[1:]})
					default:
						patchLines = append(patchLines, patchLine{kind: patchLineContext, prefix: " ", text: cur})
					}
					havePrintedLines = true
					i++
				}
			}
		finishUpdate:
			patchLines = cleanPatchLines(patchLines)
			kind := patchChangeEdit
			if moveTo != "" && len(patchLines) == 0 {
				kind = patchChangeRenameOnly
			}
			changes = append(changes, patchChange{
				kind:   kind,
				path:   path,
				toPath: moveTo,
				lines:  patchLines,
			})
		default:
			i++
		}
	}

	if len(changes) == 0 {
		return nil, fmt.Errorf("no patch hunks")
	}
	return changes, nil
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
