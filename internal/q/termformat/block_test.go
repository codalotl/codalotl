package termformat

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlockWidth(t *testing.T) {
	ansiInput := ANSIRed.ANSISequence(false) + "ok" + ANSIReset + "\n" +
		ANSIBlue.ANSISequence(false) + "lengthy" + ANSIReset

	windowsInput := "short\r\n" +
		ANSIGreen.ANSISequence(false) + "longer" + ANSIReset + "\r\n" +
		"last"

	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{name: "empty", input: "", expected: 0},
		{name: "singleLine", input: "hello", expected: 5},
		{name: "multipleLinesTrailingNewline", input: "one\ntwenty\nthree\n", expected: 6},
		{name: "ansiSequences", input: ansiInput, expected: 7},
		{name: "windowsLineEndings", input: windowsInput, expected: 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, BlockWidth(tt.input))
		})
	}
}

func TestBlockHeight(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{name: "empty", input: "", expected: 0},
		{name: "singleLine", input: "hello", expected: 1},
		{name: "multipleLines", input: "one\ntwo\nthree", expected: 3},
		{name: "trailingNewline", input: "one\ntwo\n", expected: 3},
		{name: "onlyNewlines", input: "\n\n", expected: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, BlockHeight(tt.input))
		})
	}
}

func TestBlockNormalizeWidthNaive(t *testing.T) {
	input := "short\nlonger"
	expected := "short \nlonger"

	require.Equal(t, expected, BlockNormalizeWidth(input, BlockNormalizeModeNaive))
}

func TestBlockNormalizeWidthExtend(t *testing.T) {
	first := ANSIRed.ANSISequence(false) + "hi" + ANSIReset
	second := "plain"

	expected := ANSIRed.ANSISequence(false) + "hi   " + ANSIReset + "\nplain"

	require.Equal(t, expected, BlockNormalizeWidth(first+"\n"+second, BlockNormalizeModeExtend))
}

func TestBlockNormalizeWidthTerminate(t *testing.T) {
	first := ANSIRed.ANSISequence(false) + "hi"
	second := "world"

	expected := ANSIRed.ANSISequence(false) + "hi" + ANSIReset + "   " + "\n" + ANSIRed.ANSISequence(false) + "world" + ANSIReset

	require.Equal(t, expected, BlockNormalizeWidth(first+"\n"+second, BlockNormalizeModeTerminate))
}

func TestBlockStyleWrapNormalizesInput(t *testing.T) {
	bs := BlockStyle{}
	input := "a\nbbb"

	require.Equal(t, "a  \nbbb", bs.Apply(input))
}

func TestBlockStyleWrapPadding(t *testing.T) {
	bs := BlockStyle{
		PaddingLeft:   2,
		PaddingRight:  1,
		PaddingTop:    1,
		PaddingBottom: 1,
	}

	expected := strings.Join([]string{
		"     ",
		"  Hi ",
		"     ",
	}, "\n")

	require.Equal(t, expected, bs.Apply("Hi"))
}

func TestBlockStyleWrapMarginBorderColors(t *testing.T) {
	bs := BlockStyle{
		MarginLeft:        1,
		MarginRight:       2,
		MarginTop:         1,
		MarginBottom:      1,
		PaddingLeft:       1,
		PaddingRight:      1,
		BorderStyle:       BorderStyleBasic,
		BorderForeground:  ANSIRed,
		BorderBackground:  ANSIBrightBlack,
		PaddingBackground: ANSIBlue,
		MarginBackground:  ANSIYellow,
	}

	input := "X"
	result := bs.Apply(input)

	normalized := BlockNormalizeWidth(input, bs.BlockNormalizeMode)
	contentWidth := BlockWidth(normalized)
	innerWidth := contentWidth + bs.PaddingLeft + bs.PaddingRight
	coreWidth := innerWidth + 2 // border left/right
	totalWidth := bs.MarginLeft + coreWidth + bs.MarginRight

	marginStyle := Style{Background: bs.MarginBackground}
	marginLine := marginStyle.Wrap(strings.Repeat(" ", totalWidth))
	marginLeftSegment := marginStyle.Wrap(strings.Repeat(" ", bs.MarginLeft))
	marginRightSegment := marginStyle.Wrap(strings.Repeat(" ", bs.MarginRight))

	paddingStyle := Style{Background: bs.PaddingBackground}
	leftPadding := paddingStyle.Wrap(strings.Repeat(" ", bs.PaddingLeft))
	rightPadding := paddingStyle.Wrap(strings.Repeat(" ", bs.PaddingRight))

	borderStyle := Style{
		Foreground: bs.BorderForeground,
		Background: bs.BorderBackground,
	}
	topBorderCore := string(borderNormal.topLeft) + strings.Repeat(string(borderNormal.top), innerWidth) + string(borderNormal.topRight)
	topBorderLine := marginLeftSegment + borderStyle.Wrap(topBorderCore) + marginRightSegment

	borderLeft := borderStyle.Wrap(string(borderNormal.left))
	borderRight := borderStyle.Wrap(string(borderNormal.right))
	contentLine := marginLeftSegment + borderLeft + leftPadding + normalized + rightPadding + borderRight + marginRightSegment

	bottomBorderCore := string(borderNormal.bottomLeft) + strings.Repeat(string(borderNormal.bottom), innerWidth) + string(borderNormal.bottomRight)
	bottomBorderLine := marginLeftSegment + borderStyle.Wrap(bottomBorderCore) + marginRightSegment

	expected := strings.Join([]string{
		marginLine,
		topBorderLine,
		contentLine,
		bottomBorderLine,
		marginLine,
	}, "\n")

	require.Equal(t, expected, result)
}

func TestBlockStyleWrapTextBackground(t *testing.T) {
	bs := BlockStyle{
		TextBackground: ANSIBlue,
	}

	input := "Hi\nX"
	result := bs.Apply(input)

	require.Equal(t, "\x1b[44mHi\x1b[0m\n\x1b[44mX \x1b[0m", result)
}

func TestBlockStyleWrapWidthTextBackground(t *testing.T) {
	bs := BlockStyle{
		TotalWidth:     20,
		TextBackground: ANSIBrightCyan,
	}

	input := "Hi"
	result := bs.Apply(input)

	assert.Equal(t, "\x1b[106mHi                  \x1b[0m", result)
}

// func escapeForLog(s string) string {
// 	var b strings.Builder
// 	for _, r := range s {
// 		if unicode.IsPrint(r) || r == '\n' || r == '\r' || r == '\t' {
// 			b.WriteRune(r)
// 		} else {
// 			if r < 0x100 {
// 				b.WriteString(fmt.Sprintf("\\x%02x", r))
// 			} else {
// 				b.WriteString(fmt.Sprintf("\\u%04x", r))
// 			}
// 		}
// 	}
// 	return b.String()
// }

func TestBlockStyleApplyTotalWidthTextBackgroundMultipleLines(t *testing.T) {
	bs := BlockStyle{
		TotalWidth:     8,
		TextBackground: ANSIBlue,
	}

	input := "abc\nZ"
	result := bs.Apply(input)

	require.Equal(t, "\x1b[44mabc     \x1b[0m\n\x1b[44mZ       \x1b[0m", result)
}

func TestBlockStyleApplyTotalWidthTextBackgroundMultipleLinesWithBorder(t *testing.T) {
	bs := BlockStyle{
		TotalWidth:       8,
		TextBackground:   ANSIBlue,
		BorderStyle:      BorderStyleBasic,
		BorderBackground: ANSIRed,
		MinTotalHeight:   5,
	}

	input := "abc\nZ"
	result := bs.Apply(input)

	require.Equal(t,
		"\x1b[41m┌──────┐\x1b[0m\n"+
			"\x1b[41m│\x1b[0m\x1b[44mabc   \x1b[0m\x1b[41m│\x1b[0m\n"+
			"\x1b[41m│\x1b[0m\x1b[44mZ     \x1b[0m\x1b[41m│\x1b[0m\n"+
			"\x1b[41m│\x1b[0m\x1b[44m      \x1b[0m\x1b[41m│\x1b[0m\n"+
			"\x1b[41m└──────┘\x1b[0m",
		result)
}

func TestBlockStyleApplyToColoredText(t *testing.T) {
	bs := BlockStyle{
		TotalWidth:       8,
		TextBackground:   ANSIBlue,
		BorderStyle:      BorderStyleBasic,
		BorderBackground: ANSIRed,
		MinTotalHeight:   5,
		// BlockNormalizeMode: BlockNormalizeModeTerminate,
	}

	input := Style{Foreground: ANSIBrightCyan}.Wrap("abc\nZ")
	result := bs.Apply(input)

	// fmt.Println(result)
	// fmt.Printf("%s", escapeForLog(result))

	require.Equal(t,
		"\x1b[41m┌──────┐\x1b[0m\n"+
			"\x1b[41m│\x1b[0m\x1b[96m\x1b[44mabc   \x1b[0m\x1b[41m│\x1b[0m\n"+
			"\x1b[41m│\x1b[0m\x1b[96m\x1b[44mZ\x1b[0m\x1b[44m     \x1b[0m\x1b[41m│\x1b[0m\n"+
			"\x1b[41m│\x1b[0m\x1b[44m      \x1b[0m\x1b[41m│\x1b[0m\n"+
			"\x1b[41m└──────┘\x1b[0m",
		result)
}

func TestBlockStyleWrapMaxTotalWidthExpand(t *testing.T) {
	bs := BlockStyle{
		TotalWidth: 6,
	}

	result := bs.Apply("Hi")
	require.Equal(t, "Hi    ", result)
	require.Equal(t, 6, BlockWidth(result))
}

func TestBlockStyleWrapMaxTotalWidthWraps(t *testing.T) {
	bs := BlockStyle{
		TotalWidth: 4,
	}

	result := bs.Apply("HelloWorld")
	expected := strings.Join([]string{
		"Hell",
		"oWor",
		"ld  ",
	}, "\n")

	require.Equal(t, expected, result)
	require.Equal(t, 4, BlockWidth(result))
}

func TestBlockStyleWrapMaxTotalWidthWithPaddingAndBorder(t *testing.T) {
	bs := BlockStyle{
		TotalWidth:   10,
		PaddingLeft:  1,
		PaddingRight: 1,
		BorderStyle:  BorderStyleBasic,
	}

	result := bs.Apply("Hi")
	require.Equal(t, 10, BlockWidth(result))

	lines := strings.Split(result, "\n")
	require.Equal(t, 3, len(lines))
	require.Equal(t, 10, TextWidthWithANSICodes(lines[0]))
	require.Equal(t, 10, TextWidthWithANSICodes(lines[1]))
	require.Equal(t, 10, TextWidthWithANSICodes(lines[2]))
}

func TestBlockStyleWrapMaxTotalWidthExtendMode(t *testing.T) {
	styled := ANSIRed.ANSISequence(false) + "Hi" + ANSIReset

	bs := BlockStyle{
		BlockNormalizeMode: BlockNormalizeModeExtend,
		TotalWidth:         6,
	}

	result := bs.Apply(styled)
	expected := ANSIRed.ANSISequence(false) + "Hi    " + ANSIReset

	require.Equal(t, expected, result)
	require.Equal(t, 6, BlockWidth(result))
}

func TestBlockStyleWrapMinTotalHeightNaive(t *testing.T) {
	bs := BlockStyle{
		MinTotalHeight: 3,
	}

	result := bs.Apply("Hi")
	require.Equal(t, "Hi\n  \n  ", result)
}

func TestBlockStyleWrapMinTotalHeightExtend(t *testing.T) {
	styled := ANSIRed.ANSISequence(false) + "Hi"

	bs := BlockStyle{
		BlockNormalizeMode: BlockNormalizeModeExtend,
		MinTotalHeight:     2,
	}

	expected := ANSIRed.ANSISequence(false) + "Hi" + ANSIReset + "\n" + ANSIRed.ANSISequence(false) + "  " + ANSIReset

	require.Equal(t, expected, bs.Apply(styled))
}

func TestBlockStyleWrapMinTotalHeightTerminate(t *testing.T) {
	styled := ANSIRed.ANSISequence(false) + "Hi"

	bs := BlockStyle{
		BlockNormalizeMode: BlockNormalizeModeTerminate,
		MinTotalHeight:     2,
	}

	require.Equal(t, ANSIRed.ANSISequence(false)+"Hi"+ANSIReset+"\n  ", bs.Apply(styled))
}

func TestBlockStyleWrapMaxTotalWidthStructuralPanic(t *testing.T) {
	bs := BlockStyle{
		TotalWidth:  1,
		PaddingLeft: 2,
	}

	require.PanicsWithValue(t, "termformat: MaxTotalWidth cannot contain margin, padding, and border", func() {
		bs.Apply("hi")
	})
}

func TestBlockStyleWrapMaxTotalWidthZeroContentPanic(t *testing.T) {
	bs := BlockStyle{
		TotalWidth:   2,
		PaddingLeft:  1,
		PaddingRight: 1,
	}

	require.PanicsWithValue(t, "termformat: MaxTotalWidth leaves no room for content", func() {
		bs.Apply("a")
	})
}

func TestBlockStylePerLine(t *testing.T) {
	reset := ANSIReset
	shortReset := "\x1b[m"
	bold := Style{Bold: StyleSetOn}.OpeningControlCodes()
	red := ANSIRed.ANSISequence(false)
	blue := ANSIBlue.ANSISequence(false)
	osc := "\x1b]0;title\x07"

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "Empty", input: "", expected: ""},
		{name: "Plain", input: "hi", expected: "hi"},
		{name: "StyledWithReset", input: bold + "hi" + reset, expected: bold + "hi" + reset},
		{name: "StyledWithoutReset", input: bold + "hi", expected: bold + "hi" + reset},
		{name: "ColorAcrossLinesWithReset", input: red + "hello\nworld" + reset, expected: red + "hello" + reset + "\n" + red + "world" + reset},
		{name: "ColorAcrossLinesWithoutReset", input: red + "hello\nworld", expected: red + "hello" + reset + "\n" + red + "world" + reset},
		{name: "StyledSegmentThenPlain", input: red + "hello" + reset + " world", expected: red + "hello" + reset + " world"},
		{name: "StyleChangeBeforeNewline", input: red + "he" + blue + "llo\nworld" + reset, expected: red + "he" + blue + "llo" + reset + "\n" + blue + "world" + reset},
		{name: "ShortResetStopsCarry", input: red + "hi" + shortReset + "\nthere", expected: red + "hi" + shortReset + "\nthere"},
		{name: "WindowsLineEndings", input: red + "hi\r\nthere", expected: red + "hi" + reset + "\r\n" + red + "there" + reset},
		{name: "EmptyLineCarriesStyle", input: red + "\nhi" + reset, expected: red + reset + "\n" + red + "hi" + reset},
		{name: "TrailingNewlineWithStyle", input: red + "hi\n", expected: red + "hi" + reset + "\n" + red + reset},
		{name: "NonSGREscapePreserved", input: osc + red + "hi\nthere" + reset, expected: osc + red + "hi" + reset + "\n" + red + "there" + reset},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, BlockStylePerLine(tt.input))
		})
	}
}

func TestBlockStylePerLineSkipsAllocWhenUnchanged(t *testing.T) {
	input := ANSIRed.ANSISequence(false) + "hi" + ANSIReset

	require.Equal(t, input, BlockStylePerLine(input))

	var result string
	allocs := testing.AllocsPerRun(100, func() {
		result = BlockStylePerLine(input)
	})

	require.Equal(t, input, result)
	require.Zero(t, allocs)
}
