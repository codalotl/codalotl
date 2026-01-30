package termformat

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLayoutBasics(t *testing.T) {
	b1 := BlockStyle{TotalWidth: 10, TextBackground: ANSICyan}.Apply("b1a\nb2a")
	b2 := BlockStyle{TotalWidth: 20, TextBackground: ANSIRed}.Apply("b2")
	blocks := []LayoutBlock{
		{Block: b1, X: 0, Y: 0},
		{Block: b2, X: 10, Y: 0},
	}
	result, err := Layout(blocks, nil)
	require.NoError(t, err)
	assert.Equal(t, "\x1b[46mb1a       \x1b[0m\x1b[41mb2                  \x1b[0m\n\x1b[46mb2a       \x1b[0m                    ", result)
}

func TestLayoutRedundantFirstBlock(t *testing.T) {
	b1 := "\x1b[48;5;223m\x1b[48;5;223m\x1b[48;5;223mXXX       \x1b[0m"
	b2 := BlockStyle{TotalWidth: 20, TextBackground: ANSIRed}.Apply("YY")
	blocks := []LayoutBlock{
		{Block: b1, X: 0, Y: 0},
		{Block: b2, X: 10, Y: 0},
	}
	result, err := Layout(blocks, nil)
	require.NoError(t, err)
	assert.Equal(t, "\x1b[48;5;223m\x1b[48;5;223m\x1b[48;5;223mXXX       \x1b[0m\x1b[41mYY                  \x1b[0m", result)
}

func TestLayoutNormalizesBlocks(t *testing.T) {
	input := ANSIRed.ANSISequence(false) + "hi\nX" + ANSIReset
	expected := BlockNormalizeWidth(BlockStylePerLine(input), BlockNormalizeModeNaive)

	result, err := Layout([]LayoutBlock{
		{Block: input},
	}, NoColor{})

	require.NoError(t, err)
	require.Equal(t, expected, result)
}

func TestLayoutOffsetsWithoutFill(t *testing.T) {
	result, err := Layout([]LayoutBlock{
		{Block: "X", X: 1, Y: 1},
	}, nil)

	require.NoError(t, err)

	lines := strings.Split(result, "\n")
	require.Len(t, lines, 2)
	require.Equal(t, "  ", lines[0])
	require.Equal(t, " X", lines[1])
}

func TestLayoutAppliesFillBackground(t *testing.T) {
	fillStyle := Style{Background: ANSIBlue}
	fill := func(width int) string {
		return fillStyle.Wrap(strings.Repeat(" ", width))
	}

	result, err := Layout([]LayoutBlock{
		{Block: "A", X: 0, Y: 0},
		{Block: "B", X: 3, Y: 0},
		{Block: "C", X: 1, Y: 2},
	}, ANSIBlue)

	require.NoError(t, err)

	expected := strings.Join([]string{
		"A" + fill(2) + "B",
		fill(4),
		fill(1) + "C" + fill(2),
	}, "\n")

	require.Equal(t, expected, result)
}

func TestLayoutOverlappingBlocks(t *testing.T) {
	_, err := Layout([]LayoutBlock{
		{Block: "One", X: 0, Y: 0},
		{Block: "Two", X: 1, Y: 0},
	}, nil)

	require.EqualError(t, err, "termformat: blocks overlap")
}

func TestOverlayCentersDialog(t *testing.T) {
	background := strings.Join([]string{
		"12345",
		"67890",
	}, "\n")

	dialog := strings.Join([]string{
		"X",
		"Y",
	}, "\n")

	result := Overlay(dialog, background, OverlayPosition{})

	expected := strings.Join([]string{
		"12X45",
		"67Y90",
	}, "\n")

	require.Equal(t, expected, result)
}

func TestOverlayResumesBackgroundStyles(t *testing.T) {
	background := ANSIRed.ANSISequence(false) + "ABCDEF" + ANSIReset
	dialog := "ZZ"

	result := Overlay(dialog, background, OverlayPosition{
		AutoX: OverlayRelativePositionCenter,
	})

	expected := ANSIRed.ANSISequence(false) + "AB" + ANSIReset + "ZZ" + ANSIRed.ANSISequence(false) + "EF" + ANSIReset
	require.Equal(t, expected, result)
}

func TestOverlayTruncatesDialog(t *testing.T) {
	background := strings.Join([]string{
		"abc",
		"def",
	}, "\n")

	dialog := strings.Join([]string{
		"QRSTU",
		"VWXYZ",
		"mnopq",
	}, "\n")

	result := Overlay(dialog, background, OverlayPosition{})

	expected := strings.Join([]string{
		"QRS",
		"VWX",
	}, "\n")

	require.Equal(t, expected, result)
}
