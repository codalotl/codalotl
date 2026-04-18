package agentformatter

import (
	"fmt"
	"strings"

	"github.com/codalotl/codalotl/internal/llmstream"
)

// RenderPlainTextBlock converts a semantic llmstream.Block into unstyled text.
// It is used by consumers that need to substitute a descendant subagent final
// message without introducing a separate presentation pipeline.
func RenderPlainTextBlock(block llmstream.Block) string {
	switch body := block.(type) {
	case llmstream.Paragraph:
		return renderPlainTextParagraph(body)
	case *llmstream.Paragraph:
		if body != nil {
			return renderPlainTextParagraph(*body)
		}
	case llmstream.Checklist:
		return renderPlainTextChecklist(body)
	case *llmstream.Checklist:
		if body != nil {
			return renderPlainTextChecklist(*body)
		}
	case llmstream.Output:
		return renderPlainTextOutput(body)
	case *llmstream.Output:
		if body != nil {
			return renderPlainTextOutput(*body)
		}
	case llmstream.Diff:
		return renderPlainTextDiff(body)
	case *llmstream.Diff:
		if body != nil {
			return renderPlainTextDiff(*body)
		}
	}
	return ""
}

func renderPlainTextParagraph(paragraph llmstream.Paragraph) string {
	lines := make([]string, 0, len(paragraph.Lines))
	for _, line := range paragraph.Lines {
		lines = append(lines, renderPlainTextLine(line))
	}
	return strings.Join(lines, "\n")
}

func renderPlainTextChecklist(checklist llmstream.Checklist) string {
	lines := make([]string, 0, len(checklist.Items)+1)
	if overview := renderPlainTextLine(checklist.Overview); overview != "" {
		lines = append(lines, overview)
	}
	for _, item := range checklist.Items {
		lines = append(lines, checklistMarker(item.Status)+" "+renderPlainTextLine(item.Line))
	}
	return strings.Join(lines, "\n")
}

func checklistMarker(status llmstream.ChecklistStatus) string {
	switch status {
	case llmstream.ChecklistStatusCompleted:
		return "[x]"
	case llmstream.ChecklistStatusInProgress:
		return "[-]"
	default:
		return "[ ]"
	}
}

func renderPlainTextOutput(output llmstream.Output) string {
	lines := append([]string(nil), output.Lines...)
	if output.OmittedLineCount > 0 {
		lines = append(lines, fmt.Sprintf("... +%d lines", output.OmittedLineCount))
	}
	return strings.Join(lines, "\n")
}

func renderPlainTextDiff(diff llmstream.Diff) string {
	var lines []string
	for i, edit := range diff.Edits {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, diffHeader(edit))
		for _, line := range edit.Lines {
			lines = append(lines, diffLinePrefix(line.Kind)+line.Text)
		}
		if edit.Error != nil {
			if rendered := renderPlainTextLine(*edit.Error); rendered != "" {
				lines = append(lines, "Error: "+rendered)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func diffHeader(edit llmstream.DiffEdit) string {
	switch edit.Kind {
	case llmstream.DiffEditKindAdd:
		return "Add " + edit.NewPath
	case llmstream.DiffEditKindDelete:
		return "Delete " + edit.OldPath
	case llmstream.DiffEditKindRename:
		return "Rename " + edit.OldPath + " -> " + edit.NewPath
	default:
		path := edit.NewPath
		if path == "" {
			path = edit.OldPath
		}
		return "Edit " + path
	}
}

func diffLinePrefix(kind llmstream.DiffLineKind) string {
	switch kind {
	case llmstream.DiffLineKindAdd:
		return "+"
	case llmstream.DiffLineKindDelete:
		return "-"
	case llmstream.DiffLineKindOmitted:
		return "..."
	default:
		return " "
	}
}

func renderPlainTextLine(line llmstream.Line) string {
	if len(line.Segments) == 0 {
		return ""
	}

	var b strings.Builder
	wrote := false
	for _, seg := range line.Segments {
		if seg.Text == "" {
			continue
		}
		if line.JoinWithSpace && wrote {
			b.WriteByte(' ')
		}
		b.WriteString(seg.Text)
		wrote = true
	}
	return b.String()
}
