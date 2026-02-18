package tuicontrols

import (
	"strings"

	"github.com/codalotl/codalotl/internal/q/uni"
)

// WrapPromptedText wraps contents into display lines the same way TextArea does, including prompt and hanging indent.
//
//   - prompt is the prefix shown on the first display line.
//   - width is the available display width (in terminal cells) of the full line, including prompt.
//   - contents is the text to wrap; it may contain '\n' logical newlines.
//
// The returned lines:
//   - contain no ANSI styling
//   - have prompt on the first display line
//   - have a hanging indent (spaces) on subsequent display lines so text aligns with the first typed column after the prompt
//
// contents is assumed to have already been sanitized the same way TextArea stores it (tabs expanded, '\r' removed).
func WrapPromptedText(prompt string, width int, contents string) []string {
	if width <= 0 {
		width = 1
	}

	promptWidthCells := 0
	if prompt != "" {
		promptWidthCells = uni.TextWidth(prompt, nil)
		if promptWidthCells < 0 {
			promptWidthCells = 0
		}
	}

	firstPrefix := ""
	if prompt != "" {
		firstPrefix = cutPlainStringToWidth(prompt, width)
	}

	indentCells := promptWidthCells
	if indentCells > width {
		indentCells = width
	}
	indent := ""
	if indentCells > 0 {
		indent = strings.Repeat(" ", indentCells)
	}

	availTextWidth := width - promptWidthCells
	if availTextWidth < 0 {
		availTextWidth = 0
	}

	segs := buildDisplaySegments(contents, availTextWidth)
	if len(segs) == 0 {
		segs = []displaySegment{{text: "", startByte: 0, endByte: 0, endsLogicalLn: true}}
	}
	return wrapPromptedTextFromSegments(firstPrefix, indent, availTextWidth, segs)
}

func wrapPromptedTextFromSegments(promptPrefix, indentPrefix string, availTextWidth int, segs []displaySegment) []string {
	if segs == nil {
		return nil
	}
	if availTextWidth < 0 {
		availTextWidth = 0
	}

	out := make([]string, 0, len(segs))
	for i := range segs {
		prefix := indentPrefix
		if i == 0 {
			prefix = promptPrefix
		}
		out = append(out, prefix+cutPlainStringToWidth(segs[i].text, availTextWidth))
	}
	return out
}
