package termformat

import (
	"strings"

	"github.com/codalotl/codalotl/internal/q/uni"
)

// BlockWidth calculates TextWidthWithANSICodes for each line in str and returns the max value. In other words, it's the number
// of columns that printing a block of text would occupy.
func BlockWidth(str string) int {
	maxWidth := 0
	lineStart := 0

	calcWidth := func(line string) {
		width := TextWidthWithANSICodes(line)
		if width > maxWidth {
			maxWidth = width
		}
	}

	for i := 0; i < len(str); i++ {
		if str[i] == '\n' {
			calcWidth(str[lineStart:i])
			lineStart = i + 1
		}
	}

	calcWidth(str[lineStart:])

	return maxWidth
}

// BlockHeight is the number of rows in str. Note that if str has a trailing newline, str is considered to have a blank last row (it counts).
func BlockHeight(str string) int {
	if str == "" {
		return 0
	}

	height := 1
	for i := 0; i < len(str); i++ {
		if str[i] == '\n' {
			height++
		}
	}
	return height
}

type BlockNormalizeMode string

const (
	BlockNormalizeModeNaive     BlockNormalizeMode = ""
	BlockNormalizeModeTerminate BlockNormalizeMode = "terminate"
	BlockNormalizeModeExtend    BlockNormalizeMode = "extend"
)

// BlockNormalizeWidth will pad all but the longest line with spaces so that all lines are equal width.
//
// Consider a str that is styled with ANSI codes. The styles may span lines. You may or may not want those styles to apply to the spaces added.
//   - BlockNormalizeModeNaive just adds spaces to each line, with no special logic. Sometimes those spaces inherit styles, sometimes not. Best for an unstyled block.
//   - BlockNormalizeModeTerminate ensures an ANSI reset is present on each line, if that line contains ongoing styles. Spaces added have default terminal styles. If styles were terminated, they're resumed on the next line.
//   - BlockNormalizeModeExtend ensure spaces added inherit the style of the ongoing style of the line. This effectively means adding spaces before a reset, if a reset exists.
func BlockNormalizeWidth(str string, mode BlockNormalizeMode) string {
	if str == "" {
		return ""
	}

	input := str
	if mode == BlockNormalizeModeTerminate || mode == BlockNormalizeModeExtend {
		input = BlockStylePerLine(str)
	}

	lines := strings.Split(input, "\n")
	type lineInfo struct {
		core  string
		hadCR bool
		width int
	}

	lineInfos := make([]lineInfo, len(lines))
	maxWidth := 0
	for i, line := range lines {
		core := line
		hadCR := false
		if strings.HasSuffix(core, "\r") {
			hadCR = true
			core = core[:len(core)-1]
		}
		width := TextWidthWithANSICodes(core)
		if width > maxWidth {
			maxWidth = width
		}
		lineInfos[i] = lineInfo{
			core:  core,
			hadCR: hadCR,
			width: width,
		}
	}

	out := make([]string, len(lineInfos))

	for i, info := range lineInfos {
		content := info.core
		pad := maxWidth - info.width

		lineOut := content
		switch mode {
		case BlockNormalizeModeExtend:
			if pad > 0 {
				base, resets := splitTrailingResets(lineOut)
				if resets != "" {
					lineOut = base + strings.Repeat(" ", pad) + resets
				} else {
					lineOut += strings.Repeat(" ", pad)
				}
			}
		case BlockNormalizeModeTerminate:
			if pad > 0 {
				lineOut += strings.Repeat(" ", pad)
			}
		default:
			if pad > 0 {
				lineOut += strings.Repeat(" ", pad)
			}
		}

		if info.hadCR {
			lineOut += "\r"
		}
		out[i] = lineOut
	}

	return strings.Join(out, "\n")
}

// BlockStylePerLine ensures str's ANSI styles are applied and reset on a per-line basis. The returned string should be displayed identically in terminals to the original.
//
// Examples (using tags for ease of human reading - actually ANSI codes):
//   - "" -> ""
//   - "hi" -> "hi"
//   - "<bold>hi<reset>" -> "<bold>hi<reset>"
//   - "<bold>hi" -> "<bold>hi<reset>"
//   - "<red>hello\nworld<reset>" -> "<red>hello<reset>\n<red>world<reset>"
//   - "<red>hello\nworld" -> "<red>hello<reset>\n<red>world<reset>"
//   - "<red>hello<reset> world" -> "<red>hello<reset> world"
//
// The resultant styled string is easier to work with: a line is an atomic unit that can be written independently, without the rest of the block.
func BlockStylePerLine(str string) string {
	if str == "" {
		return ""
	}

	if !blockStylePerLineNeedsRewrite(str) {
		return str
	}

	lines := strings.Split(str, "\n")
	var out strings.Builder
	out.Grow(len(str) + len(lines)) // rough guess; extra len(lines) for possible resets/newlines

	startState := defaultState()

	for i, line := range lines {
		if i > 0 {
			out.WriteByte('\n')
		}

		hadCR := false
		core := line
		if strings.HasSuffix(core, "\r") {
			hadCR = true
			core = core[:len(core)-1]
		}

		if prefix := buildStateTransition(startState); prefix != "" {
			out.WriteString(prefix)
		}

		out.WriteString(core)

		endState := simulateSGRState(startState, core)
		if !endState.isDefault() {
			out.WriteString(ANSIReset)
		}

		if hadCR {
			out.WriteByte('\r')
		}

		startState = endState
	}

	return out.String()

}

func blockStylePerLineNeedsRewrite(str string) bool {
	lineStart := 0

	for lineStart <= len(str) {
		lineEnd := strings.IndexByte(str[lineStart:], '\n')
		if lineEnd == -1 {
			lineEnd = len(str)
		} else {
			lineEnd += lineStart
		}

		if !lineEndsWithDefaultState(str[lineStart:lineEnd]) {
			return true
		}

		if lineEnd == len(str) {
			break
		}
		lineStart = lineEnd + 1
	}

	return false
}

func lineEndsWithDefaultState(line string) bool {
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}

	var state simpleSGRState
	var paramsBuf [32]int

	for i := 0; i < len(line); {
		if line[i] == '\x1b' && i+1 < len(line) && line[i+1] == '[' {
			end := i + 2
			for end < len(line) && line[end] != 'm' {
				end++
			}
			if end < len(line) && line[end] == 'm' {
				if params, ok := parseSGRParametersInline(line[i+2:end], paramsBuf[:0]); ok {
					state.apply(params)
				}
				i = end + 1
				continue
			}
		}
		i++
	}

	return state.isDefault()
}

type simpleSGRState struct {
	bold          bool
	italic        bool
	underline     bool
	overline      bool
	strikeThrough bool
	reverse       bool
	fgSet         bool
	bgSet         bool
}

func (s *simpleSGRState) apply(params []int) {
	for i := 0; i < len(params); i++ {
		p := params[i]
		switch {
		case p == 0:
			*s = simpleSGRState{}
		case p == 1:
			s.bold = true
		case p == 22:
			s.bold = false
		case p == 3:
			s.italic = true
		case p == 23:
			s.italic = false
		case p == 4:
			s.underline = true
		case p == 24:
			s.underline = false
		case p == 7:
			s.reverse = true
		case p == 27:
			s.reverse = false
		case p == 9:
			s.strikeThrough = true
		case p == 29:
			s.strikeThrough = false
		case p == 53:
			s.overline = true
		case p == 55:
			s.overline = false
		case p == 39:
			s.fgSet = false
		case p == 49:
			s.bgSet = false
		case isForegroundColor(p):
			s.fgSet = true
		case isBackgroundColor(p):
			s.bgSet = true
		case p == 38:
			if advanced, next := applyExtendedColorParam(params, i); advanced {
				s.fgSet = true
				i = next
			}
		case p == 48:
			if advanced, next := applyExtendedColorParam(params, i); advanced {
				s.bgSet = true
				i = next
			}
		}
	}
}

func (s simpleSGRState) isDefault() bool {
	return !s.bold &&
		!s.italic &&
		!s.underline &&
		!s.overline &&
		!s.strikeThrough &&
		!s.reverse &&
		!s.fgSet &&
		!s.bgSet
}

func applyExtendedColorParam(params []int, idx int) (bool, int) {
	if idx+1 >= len(params) {
		return false, idx
	}
	mode := params[idx+1]
	if mode == 5 {
		if idx+2 >= len(params) {
			return false, idx
		}
		return true, idx + 2
	}
	if mode == 2 {
		if idx+4 >= len(params) {
			return false, idx
		}
		return true, idx + 4
	}
	return false, idx
}

func parseSGRParametersInline(content string, buf []int) ([]int, bool) {
	buf = buf[:0]
	if content == "" {
		return append(buf, 0), true
	}

	start := 0
	for start <= len(content) {
		end := start
		for end < len(content) && content[end] != ';' {
			end++
		}

		if end == start {
			buf = append(buf, 0)
		} else {
			val, ok := parseSGRInt(content[start:end])
			if !ok {
				return nil, false
			}
			buf = append(buf, val)
		}

		if end == len(content) {
			break
		}
		start = end + 1
	}
	return buf, true
}

func parseSGRInt(segment string) (int, bool) {
	val := 0
	for i := 0; i < len(segment); i++ {
		c := segment[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		val = val*10 + int(c-'0')
	}
	return val, true
}

func simulateSGRState(start state, text string) state {
	cur := start

	for i := 0; i < len(text); {
		if text[i] == '\x1b' && i+1 < len(text) && text[i+1] == '[' {
			end := i + 2
			for end < len(text) && text[end] != 'm' {
				end++
			}
			if end < len(text) && text[end] == 'm' {
				if params, ok := parseSGRParameters(text[i+2 : end]); ok {
					cur, _ = applyParams(cur, params)
				}
				i = end + 1
				continue
			}
		}
		i++
	}

	return cur
}

func splitTrailingResets(s string) (string, string) {
	const shortReset = "\x1b[m"
	end := len(s)
	for {
		switch {
		case end >= len(ANSIReset) && s[end-len(ANSIReset):end] == ANSIReset:
			end -= len(ANSIReset)
			continue
		case end >= len(shortReset) && s[end-len(shortReset):end] == shortReset:
			end -= len(shortReset)
			continue
		default:
			return s[:end], s[end:]
		}
	}
}

func buildStateTransition(target state) string {
	if target.isDefault() {
		return ""
	}
	var b strings.Builder
	active := defaultState()
	writeTransition(&b, target, &active, false)
	return b.String()
}

func wrapStringToWidth(str string, width int) string {
	if str == "" {
		return ""
	}
	if width <= 0 {
		return str
	}

	lines := strings.Split(str, "\n")
	out := make([]string, 0, len(lines))

	for _, line := range lines {
		hadCR := false
		core := line
		if strings.HasSuffix(core, "\r") {
			hadCR = true
			core = core[:len(core)-1]
		}

		wrapped := wrapLineToWidth(core, width)
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		if hadCR {
			wrapped[len(wrapped)-1] += "\r"
		}
		out = append(out, wrapped...)
	}

	return strings.Join(out, "\n")
}

func wrapLineToWidth(line string, width int) []string {
	if line == "" {
		return []string{""}
	}
	if width <= 0 {
		return []string{line}
	}

	var out []string
	var builder strings.Builder
	currentWidth := 0

	for i := 0; i < len(line); {
		if line[i] == '\x1b' {
			seqLen := ansiSequenceLength(line[i:])
			if seqLen == 0 {
				seqLen = 1
			}
			builder.WriteString(line[i : i+seqLen])
			i += seqLen
			continue
		}

		nextEsc := strings.IndexByte(line[i:], '\x1b')
		segmentEnd := len(line)
		if nextEsc >= 0 {
			segmentEnd = i + nextEsc
		}
		segment := line[i:segmentEnd]
		i = segmentEnd

		iter := uni.NewGraphemeIterator(segment, nil)
		for iter.Next() {
			grapheme := segment[iter.Start():iter.End()]
			gw := iter.TextWidth()

			if gw > width {
				if builder.Len() > 0 {
					out = append(out, builder.String())
					builder.Reset()
					currentWidth = 0
				}
				builder.WriteString(grapheme)
				out = append(out, builder.String())
				builder.Reset()
				currentWidth = 0
				continue
			}

			if currentWidth+gw > width && builder.Len() > 0 {
				out = append(out, builder.String())
				builder.Reset()
				currentWidth = 0
			}

			builder.WriteString(grapheme)
			currentWidth += gw

			if currentWidth == width {
				out = append(out, builder.String())
				builder.Reset()
				currentWidth = 0
			}
		}
	}

	if builder.Len() > 0 {
		out = append(out, builder.String())
	} else if len(out) == 0 {
		out = []string{""}
	}

	return out
}

func padNormalizedToWidth(str string, target int, mode BlockNormalizeMode) string {
	if target <= 0 {
		if str == "" {
			return ""
		}
		panic("termformat: unable to normalize content to MaxTotalWidth")
	}

	current := BlockWidth(str)
	if current >= target {
		return str
	}

	if str == "" {
		return strings.Repeat(" ", target)
	}

	pad := target - current
	lines := strings.Split(str, "\n")
	for i, line := range lines {
		hadCR := false
		core := line
		if strings.HasSuffix(core, "\r") {
			hadCR = true
			core = core[:len(core)-1]
		}

		switch mode {
		case BlockNormalizeModeExtend:
			base, resets := splitTrailingResets(core)
			core = base + strings.Repeat(" ", pad) + resets
		case BlockNormalizeModeTerminate:
			core += strings.Repeat(" ", pad)
		default:
			core += strings.Repeat(" ", pad)
		}

		if hadCR {
			core += "\r"
		}
		lines[i] = core
	}

	return strings.Join(lines, "\n")
}

func padNormalizedToHeight(str string, target int, lineWidth int, mode BlockNormalizeMode) string {
	if target <= 0 {
		if str == "" {
			return ""
		}
		panic("termformat: unable to normalize content to MinTotalHeight")
	}

	current := BlockHeight(str)
	if current >= target {
		return str
	}

	missing := target - current

	var lines []string
	if str != "" {
		lines = strings.Split(str, "\n")
	}

	appendCR := len(lines) > 0 && strings.HasSuffix(lines[len(lines)-1], "\r")

	fillerCore := strings.Repeat(" ", lineWidth)
	fillerLine := fillerCore

	if mode == BlockNormalizeModeExtend && len(lines) > 0 {
		lastLine := lines[len(lines)-1]
		if appendCR {
			lastLine = lastLine[:len(lastLine)-1]
		}
		base, resets := splitTrailingResets(lastLine)
		carryState := simulateSGRState(defaultState(), base)

		fillerLine = buildStateTransition(carryState) + fillerCore
		resetSeq := resets
		if resetSeq == "" && !carryState.isDefault() {
			resetSeq = ANSIReset
		}
		fillerLine += resetSeq
	} else if appendCR {
		fillerLine += "\r"
	}

	if appendCR && !strings.HasSuffix(fillerLine, "\r") {
		fillerLine += "\r"
	}

	for i := 0; i < missing; i++ {
		lines = append(lines, fillerLine)
	}

	var result string
	if len(lines) > 0 {
		result = strings.Join(lines, "\n")
	}

	if result == "" && missing > 0 {
		result = strings.Repeat("\n", missing)
	}

	return result
}

type BorderStyle int

const (
	BorderStyleNone BorderStyle = iota
	BorderStyleBasic
	BorderStyleThick
	BorderStyleInnerHalfBlock
	BorderStyleOuterHalfBlock
	BorderStyleHidden // we use the background color only
)

// BlockStyle specifies properties for Wrap.
//
// Margin goes outside the border, padding inside.
type BlockStyle struct {
	BlockNormalizeMode BlockNormalizeMode

	MarginLeft   int
	MarginRight  int
	MarginTop    int
	MarginBottom int
	Margin       int // Margin is used for Margin{Left,Right,Top,Bottom}, if that margin is 0.

	PaddingLeft   int
	PaddingRight  int
	PaddingTop    int
	PaddingBottom int
	Padding       int // Padding is used for Padding{Left,Right,Top,Bottom}, if that padding is 0.

	// If present, the final styled block will be exactly TotalWidth, including inner text, margin, padding, and border. If the text's block width + margin + padding + border is less than TotalWidth,
	// spaces will be added to each line of the text using BlockNormalizeMode until the width is achieved. If TotalWidth is too small for the text+margin+padding+border, text is wrapped and the block is
	// re-normalized using BlockNormalizeMode. If padding+margin+border is greater than TotalWidth, panic.
	TotalWidth int

	// If present, the final styled block will be at least MinTotalHeight. Rows with full of spaces will be added to text using BlockNormalizeMode until MinTotalHeight is achieved:
	//   - BlockNormalizeModeNaive: just naively add line endings (\n or \r\n) with spaces
	//   - BlockNormalizeModeTerminate: ensure styles are terminated, then add the rows.
	//   - BlockNormalizeModeExtend: ensure rows inherit the styling of the last row, just before it was reset.
	MinTotalHeight int

	// NOTE: MinTotalWidth is a logical property that might be desired at some point.
	// MaxTotalHeight is possible if we accept row truncation is okay (not obvious to me that it is).

	BorderStyle BorderStyle

	TextBackground    Color // if set, applies this bg color to the wrapped text block (including spaces added during normalization) using Style.Apply. The entire inner box (not margin/border/padding) will be this color bg.
	MarginBackground  Color // if set, bg color applied to any add margin.
	PaddingBackground Color // if set, bg color applied to any add padding.
	BorderForeground  Color // if set and BorderStyle != BorderStyleNone, fg color applied to border.
	BorderBackground  Color // if set and BorderStyle != BorderStyleNone, bg color applied to border.
}

// Apply applies the block styles to str, which may already have formatting. If str is not already of equal width per row, it will be normalized with BlockNormalizeWidth. See BlockStyle for details.
//
// The returned string's rows will all be equal width. Apply will panic if MaxTotalWidth cannot contain the specified padding/margin/border.
func (bs BlockStyle) Apply(str string) string {
	resolvedSpacing := func(explicit, fallback int) int {
		if explicit != 0 {
			if explicit < 0 {
				return 0
			}
			return explicit
		}
		if fallback < 0 {
			return 0
		}
		return fallback
	}

	marginLeft := resolvedSpacing(bs.MarginLeft, bs.Margin)
	marginRight := resolvedSpacing(bs.MarginRight, bs.Margin)
	marginTop := resolvedSpacing(bs.MarginTop, bs.Margin)
	marginBottom := resolvedSpacing(bs.MarginBottom, bs.Margin)

	paddingLeft := resolvedSpacing(bs.PaddingLeft, bs.Padding)
	paddingRight := resolvedSpacing(bs.PaddingRight, bs.Padding)
	paddingTop := resolvedSpacing(bs.PaddingTop, bs.Padding)
	paddingBottom := resolvedSpacing(bs.PaddingBottom, bs.Padding)

	hasBorder := bs.BorderStyle != BorderStyleNone
	borderWidth := 0
	if hasBorder {
		borderWidth = 2
	}

	structuralWidth := marginLeft + marginRight + paddingLeft + paddingRight + borderWidth

	maxContentWidth := -1
	if bs.TotalWidth > 0 {
		if structuralWidth > bs.TotalWidth {
			panic("termformat: MaxTotalWidth cannot contain margin, padding, and border")
		}
		maxContentWidth = bs.TotalWidth - structuralWidth
		if maxContentWidth < 0 {
			panic("termformat: MaxTotalWidth cannot contain margin, padding, and border")
		}
		if maxContentWidth == 0 && BlockWidth(str) > 0 {
			panic("termformat: MaxTotalWidth leaves no room for content")
		}
	}

	processed := str
	if maxContentWidth >= 0 && maxContentWidth < BlockWidth(str) {
		processed = wrapStringToWidth(str, maxContentWidth)
	} else if maxContentWidth == 0 {
		processed = ""
	}

	normalized := BlockNormalizeWidth(processed, bs.BlockNormalizeMode)

	if maxContentWidth >= 0 {
		contentWidth := BlockWidth(normalized)
		if contentWidth > maxContentWidth {
			panic("termformat: MaxTotalWidth too small for content")
		}
		if contentWidth < maxContentWidth {
			normalized = padNormalizedToWidth(normalized, maxContentWidth, bs.BlockNormalizeMode)
			contentWidth = BlockWidth(normalized)
			if contentWidth != maxContentWidth {
				panic("termformat: unable to normalize content to MaxTotalWidth")
			}
		}
	}

	contentWidth := BlockWidth(normalized)

	if bs.MinTotalHeight > 0 {
		contentHeight := BlockHeight(normalized)
		borderHeight := 0
		if hasBorder {
			borderHeight = 2
		}
		targetContentHeight := bs.MinTotalHeight - (paddingTop + paddingBottom + borderHeight + marginTop + marginBottom)
		if targetContentHeight < 0 {
			targetContentHeight = 0
		}
		if targetContentHeight > contentHeight {
			normalized = padNormalizedToHeight(normalized, targetContentHeight, contentWidth, bs.BlockNormalizeMode)
			contentWidth = BlockWidth(normalized)
		}
	}

	var contentLines []string
	switch {
	case normalized == "" && str == "":
		contentLines = nil
	case normalized == "":
		contentLines = []string{""}
	default:
		source := normalized
		if bs.TextBackground != nil && source != "" {
			source = BlockStylePerLine(source)
		}
		contentLines = strings.Split(source, "\n")
	}

	if bs.TextBackground != nil && len(contentLines) > 0 {
		textStyle := Style{
			Background: bs.TextBackground,
		}
		for i, line := range contentLines {
			if line == "" {
				continue
			}
			hadCR := strings.HasSuffix(line, "\r")
			core := line
			if hadCR {
				core = core[:len(core)-1]
			}
			if core == "" {
				continue
			}
			styled := textStyle.Apply(core)
			if hadCR {
				styled += "\r"
			}
			contentLines[i] = styled
		}
	}
	innerLines := make([]string, 0, len(contentLines)+paddingTop+paddingBottom)
	lineIsPadding := make([]bool, 0, len(contentLines)+paddingTop+paddingBottom)

	paddingContent := strings.Repeat(" ", contentWidth)
	for i := 0; i < paddingTop; i++ {
		innerLines = append(innerLines, paddingContent)
		lineIsPadding = append(lineIsPadding, true)
	}

	for _, line := range contentLines {
		innerLines = append(innerLines, line)
		lineIsPadding = append(lineIsPadding, false)
	}

	for i := 0; i < paddingBottom; i++ {
		innerLines = append(innerLines, paddingContent)
		lineIsPadding = append(lineIsPadding, true)
	}

	innerWidth := contentWidth + paddingLeft + paddingRight

	var borderChars border
	if hasBorder {
		switch bs.BorderStyle {
		case BorderStyleBasic:
			borderChars = borderNormal
		case BorderStyleInnerHalfBlock:
			borderChars = innerHalfBlockBorder
		case BorderStyleOuterHalfBlock:
			borderChars = outerHalfBlockBorder
		case BorderStyleThick:
			borderChars = thickBorder
		case BorderStyleHidden:
			borderChars = hiddenBorder
		default:
			borderChars = borderNormal
		}
	}

	var marginStyle *Style
	if bs.MarginBackground != nil {
		marginStyle = &Style{
			Background: bs.MarginBackground,
		}
	}

	var paddingStyle *Style
	if bs.PaddingBackground != nil {
		paddingStyle = &Style{
			Background: bs.PaddingBackground,
		}
	}

	var borderStyle *Style
	if bs.BorderForeground != nil || bs.BorderBackground != nil {
		borderStyle = &Style{
			Foreground: bs.BorderForeground,
			Background: bs.BorderBackground,
		}
	}

	wrapSegment := func(style *Style, segment string) string {
		if segment == "" {
			return ""
		}
		if style == nil {
			return segment
		}
		return style.Wrap(segment)
	}

	leftPaddingSegment := wrapSegment(paddingStyle, strings.Repeat(" ", paddingLeft))
	rightPaddingSegment := wrapSegment(paddingStyle, strings.Repeat(" ", paddingRight))

	coreLines := make([]string, 0, len(innerLines)+2)

	if hasBorder {
		horizontal := strings.Repeat(string(borderChars.top), innerWidth)
		topLine := string(borderChars.topLeft) + horizontal + string(borderChars.topRight)
		coreLines = append(coreLines, wrapSegment(borderStyle, topLine))
	}

	for idx, line := range innerLines {
		mid := line
		if lineIsPadding[idx] && paddingStyle != nil && mid != "" {
			mid = wrapSegment(paddingStyle, mid)
		}

		core := leftPaddingSegment + mid + rightPaddingSegment

		if hasBorder {
			leftBorder := wrapSegment(borderStyle, string(borderChars.left))
			rightBorder := wrapSegment(borderStyle, string(borderChars.right))
			core = leftBorder + core + rightBorder
		}

		coreLines = append(coreLines, core)
	}

	if hasBorder {
		horizontal := strings.Repeat(string(borderChars.bottom), innerWidth)
		bottomLine := string(borderChars.bottomLeft) + horizontal + string(borderChars.bottomRight)
		coreLines = append(coreLines, wrapSegment(borderStyle, bottomLine))
	}

	coreWidth := innerWidth
	if hasBorder {
		coreWidth += 2
	}

	totalWidth := marginLeft + coreWidth + marginRight
	marginLine := strings.Repeat(" ", totalWidth)
	marginLine = wrapSegment(marginStyle, marginLine)

	marginLeftSegment := wrapSegment(marginStyle, strings.Repeat(" ", marginLeft))
	marginRightSegment := wrapSegment(marginStyle, strings.Repeat(" ", marginRight))

	finalLines := make([]string, 0, marginTop+len(coreLines)+marginBottom)

	for i := 0; i < marginTop; i++ {
		finalLines = append(finalLines, marginLine)
	}

	for _, core := range coreLines {
		finalLines = append(finalLines, marginLeftSegment+core+marginRightSegment)
	}

	for i := 0; i < marginBottom; i++ {
		finalLines = append(finalLines, marginLine)
	}

	return strings.Join(finalLines, "\n")
}
