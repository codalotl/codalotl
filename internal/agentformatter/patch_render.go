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

func (f *textTUIFormatter) tuiEditToolCall(e agent.Event, width int) string {
	change, err := extractEditChange(e.ToolCall)
	if err != nil {
		return f.tuiGenericToolCall(e, width)
	}
	var builder strings.Builder
	f.renderApplyPatchChangeTUI(&builder, width, colorAccent, change, nil)
	return builder.String()
}
func (f *textTUIFormatter) cliEditToolCall(e agent.Event) string {
	change, err := extractEditChange(e.ToolCall)
	if err != nil {
		return f.cliGenericToolCall(e)
	}
	return f.renderApplyPatchChangeCLI(colorAccent, change, nil)
}
func (f *textTUIFormatter) tuiWriteToolCall(e agent.Event, width int) string {
	change, err := extractWriteChange(e.ToolCall)
	if err != nil {
		return f.tuiGenericToolCall(e, width)
	}
	var builder strings.Builder
	f.renderApplyPatchChangeTUI(&builder, width, colorAccent, change, nil)
	return builder.String()
}
func (f *textTUIFormatter) cliWriteToolCall(e agent.Event) string {
	change, err := extractWriteChange(e.ToolCall)
	if err != nil {
		return f.cliGenericToolCall(e)
	}
	return f.renderApplyPatchChangeCLI(colorAccent, change, nil)
}

func (f *textTUIFormatter) tuiEditToolComplete(e agent.Event, width int, success bool, _ string, output []toolOutputLine) string {
	change, err := extractEditChange(e.ToolCall)
	if err != nil {
		return f.tuiGenericToolComplete(e, width, success, "", output)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	var tail []toolOutputLine
	if !success {
		tail = output
	}
	var builder strings.Builder
	f.renderApplyPatchChangeTUI(&builder, width, bullet, change, tail)
	return builder.String()
}
func (f *textTUIFormatter) cliEditToolComplete(e agent.Event, success bool, _ string, output []toolOutputLine) string {
	change, err := extractEditChange(e.ToolCall)
	if err != nil {
		return f.cliGenericToolComplete(e, success, "", output)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	var tail []toolOutputLine
	if !success {
		tail = output
	}
	return f.renderApplyPatchChangeCLI(bullet, change, tail)
}
func (f *textTUIFormatter) tuiWriteToolComplete(e agent.Event, width int, success bool, _ string, output []toolOutputLine) string {
	change, err := extractWriteChange(e.ToolCall)
	if err != nil {
		return f.tuiGenericToolComplete(e, width, success, "", output)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	var tail []toolOutputLine
	if !success {
		tail = output
	}
	var builder strings.Builder
	f.renderApplyPatchChangeTUI(&builder, width, bullet, change, tail)
	return builder.String()
}
func (f *textTUIFormatter) cliWriteToolComplete(e agent.Event, success bool, _ string, output []toolOutputLine) string {
	change, err := extractWriteChange(e.ToolCall)
	if err != nil {
		return f.cliGenericToolComplete(e, success, "", output)
	}
	bullet := colorGreen
	if !success {
		bullet = colorRed
	}
	var tail []toolOutputLine
	if !success {
		tail = output
	}
	return f.renderApplyPatchChangeCLI(bullet, change, tail)
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
		if change.replaceAll {
			segments = append(segments, textSegment{text: " (replace all)", style: runeStyle{color: colorAccent}})
		}
		return segments
	}
}

func patchChangeFromPresentedDiffEdit(edit llmstream.DiffEdit) patchChange {
	change := patchChange{
		path:   sanitizeText(firstNonEmpty(edit.OldPath, edit.NewPath)),
		toPath: sanitizeText(edit.NewPath),
		lines:  patchLinesFromPresentedDiff(edit.Lines),
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
func splitToolTextLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
func extractEditChange(call *llmstream.ToolCall) (patchChange, error) {
	if call == nil {
		return patchChange{}, fmt.Errorf("missing tool call")
	}
	var payload struct {
		Path       string `json:"path"`
		FilePath   string `json:"file_path"`
		OldString  string `json:"old_string"`
		OldText    string `json:"old_text"`
		Find       string `json:"find"`
		NewString  string `json:"new_string"`
		NewText    string `json:"new_text"`
		Replace    string `json:"replace"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(call.Input)), &payload); err != nil {
		return patchChange{}, err
	}
	path := firstNonEmpty(payload.Path, payload.FilePath)
	if path == "" {
		return patchChange{}, fmt.Errorf("missing path")
	}
	oldText := firstNonEmpty(payload.OldString, payload.OldText, payload.Find)
	newText := firstNonEmpty(payload.NewString, payload.NewText, payload.Replace)
	lines := make([]patchLine, 0)
	for _, line := range splitToolTextLines(oldText) {
		lines = append(lines, patchLine{kind: patchLineRemove, prefix: "- ", text: line})
	}
	for _, line := range splitToolTextLines(newText) {
		lines = append(lines, patchLine{kind: patchLineAdd, prefix: "+ ", text: line})
	}
	return patchChange{
		kind:       patchChangeEdit,
		path:       sanitizeText(path),
		replaceAll: payload.ReplaceAll,
		lines:      lines,
	}, nil
}
func extractWriteChange(call *llmstream.ToolCall) (patchChange, error) {
	if call == nil {
		return patchChange{}, fmt.Errorf("missing tool call")
	}
	var payload struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
		Contents string `json:"contents"`
		Text     string `json:"text"`
		FileText string `json:"file_text"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(call.Input)), &payload); err != nil {
		return patchChange{}, err
	}
	path := firstNonEmpty(payload.Path, payload.FilePath)
	if path == "" {
		return patchChange{}, fmt.Errorf("missing path")
	}
	content := firstNonEmpty(payload.Content, payload.Contents, payload.Text, payload.FileText)
	lines := make([]patchLine, 0)
	for _, line := range splitToolTextLines(content) {
		lines = append(lines, patchLine{kind: patchLineAdd, prefix: "+ ", text: line})
	}
	return patchChange{
		kind:  patchChangeAdd,
		path:  sanitizeText(path),
		lines: lines,
	}, nil
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
