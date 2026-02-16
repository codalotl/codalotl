package specmd

import (
	"bytes"
	"errors"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"strings"
)

type parsedMarkdown struct {
	allGoFences         []goFenceBlock
	goFencesInPublicAPI []goFenceBlock
}
type goFenceBlock struct {
	info             string
	code             string
	multiLine        bool
	contentStart     int // byte offset in source where code content starts
	contentEnd       int // byte offset in source where code content ends
	contentStartLine int // 1-based line number in source where code content starts
	inPublicAPI      bool
	hasAPIFlag       bool
}

func parseMarkdown(src []byte) (*parsedMarkdown, error) {
	if err := validateTripleBacktickFences(src); err != nil {
		return nil, err
	}
	md := goldmark.New()
	root := md.Parser().Parse(text.NewReader(src))
	if root == nil {
		return nil, errors.New("specmd: parse markdown: nil document")
	}
	blocks, err := collectGoFencesWithContext(src, root)
	if err != nil {
		return nil, err
	}
	out := &parsedMarkdown{}
	out.allGoFences = blocks
	for _, b := range blocks {
		if b.hasAPIFlag || b.inPublicAPI {
			out.goFencesInPublicAPI = append(out.goFencesInPublicAPI, b)
		}
	}
	return out, nil
}
func validateTripleBacktickFences(src []byte) error {
	// Goldmark will happily treat an unterminated fenced code block as running to EOF.
	// We treat that as invalid markdown for our SPEC.md processing purposes.
	type fence struct {
		ticks int
	}
	var stack []fence
	lines := bytes.Split(src, []byte("\n"))
	for _, line := range lines {
		trim := bytes.TrimLeft(line, " \t")
		if len(trim) < 3 || trim[0] != '`' {
			continue
		}
		n := countLeading(trim, '`')
		if n < 3 {
			continue
		}
		// A fence line. Toggle open/close depending on current state.
		if len(stack) == 0 {
			stack = append(stack, fence{ticks: n})
			continue
		}
		// Close if the fence has at least as many backticks as the opener.
		if n >= stack[len(stack)-1].ticks {
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) != 0 {
		return errors.New("specmd: parse markdown: unterminated ``` fence")
	}
	return nil
}
func countLeading(b []byte, c byte) int {
	n := 0
	for n < len(b) && b[n] == c {
		n++
	}
	return n
}
func collectGoFencesWithContext(src []byte, root ast.Node) ([]goFenceBlock, error) {
	var blocks []goFenceBlock
	type publicSection struct {
		level int
	}
	var public *publicSection
	ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if h, ok := n.(*ast.Heading); ok {
			level := h.Level
			text := headingText(src, h)
			if strings.Contains(strings.ToLower(text), "public api") {
				public = &publicSection{level: level}
				return ast.WalkContinue, nil
			}
			// If we're in a public section, an equal-or-higher-level heading ends it.
			if public != nil && level <= public.level {
				public = nil
			}
			return ast.WalkContinue, nil
		}
		fcb, ok := n.(*ast.FencedCodeBlock)
		if !ok {
			return ast.WalkContinue, nil
		}
		info := ""
		if fcb.Info != nil {
			info = string(fcb.Info.Value(src))
		}
		if !isGoInfoString(info) {
			return ast.WalkContinue, nil
		}
		code, start, end := fencedCodeContent(src, fcb)
		multiLine := fcb.Lines() != nil && fcb.Lines().Len() >= 2
		startLine := 1
		if start >= 0 {
			startLine = 1 + bytes.Count(src[:start], []byte("\n"))
		}
		hasAPIFlag := strings.Contains(info, "{api}")
		blocks = append(blocks, goFenceBlock{
			info:             info,
			code:             code,
			multiLine:        multiLine,
			contentStart:     start,
			contentEnd:       end,
			contentStartLine: startLine,
			inPublicAPI:      public != nil,
			hasAPIFlag:       hasAPIFlag,
		})
		return ast.WalkContinue, nil
	})
	return blocks, nil
}
func headingText(src []byte, h *ast.Heading) string {
	lines := h.Lines()
	if lines == nil || lines.Len() == 0 {
		return ""
	}
	seg := lines.At(0)
	if seg.Stop <= seg.Start || seg.Stop > len(src) {
		return ""
	}
	line := src[seg.Start:seg.Stop]
	line = bytes.TrimRight(line, "\r\n")
	line = bytes.TrimLeft(line, " \t")
	if len(line) == 0 {
		return ""
	}
	if line[0] == '#' {
		i := 0
		for i < len(line) && line[i] == '#' {
			i++
		}
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		line = line[i:]
		// Trim optional trailing hashes.
		s := strings.TrimSpace(string(line))
		s = strings.TrimRight(s, "#")
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(string(line))
}
func isGoInfoString(info string) bool {
	if len(info) < 2 {
		return false
	}
	if info[0] != 'g' || info[1] != 'o' {
		return false
	}
	if len(info) == 2 {
		return true
	}
	switch info[2] {
	case ' ', '\t', '{', '\r', '\n':
		return true
	default:
		return false
	}
}
func fencedCodeContent(src []byte, fcb *ast.FencedCodeBlock) (code string, start int, end int) {
	lines := fcb.Lines()
	if lines == nil || lines.Len() == 0 {
		return "", -1, -1
	}
	start = -1
	end = -1
	var buf bytes.Buffer
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		if seg.Start < 0 || seg.Stop < seg.Start || seg.Stop > len(src) {
			continue
		}
		if start == -1 || seg.Start < start {
			start = seg.Start
		}
		if seg.Stop > end {
			end = seg.Stop
		}
		buf.Write(src[seg.Start:seg.Stop])
	}
	return buf.String(), start, end
}
