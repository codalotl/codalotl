package termformat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextWidthWithANSICodesPlain(t *testing.T) {
	require.Equal(t, 11, TextWidthWithANSICodes("hello world"))
}

func TestTextWidthWithANSICodesSGR(t *testing.T) {
	colored := ANSIRed.ANSISequence(false) + "世a" + ANSIReset + "!"
	require.Equal(t, 4, TextWidthWithANSICodes(colored))
}

func TestTextWidthWithANSICodesOSCBELTerminator(t *testing.T) {
	hyperlink := "\x1b]8;;https://example.com\x07link\x1b]8;;\x07"
	require.Equal(t, 4, TextWidthWithANSICodes(hyperlink))
}

func TestTextWidthWithANSICodesOSCSTTerminator(t *testing.T) {
	hyperlink := "\x1b]8;;https://example.com\x1b\\label\x1b]8;;\x1b\\"
	require.Equal(t, 5, TextWidthWithANSICodes(hyperlink))
}

func TestTextWidthWithANSICodesDefaultEscape(t *testing.T) {
	require.Equal(t, 2, TextWidthWithANSICodes("ok\x1bc"))
}

func TestTextWidthWithANSICodesNewlines(t *testing.T) {
	assert.Equal(t, 0, TextWidthWithANSICodes(""))
	assert.Equal(t, 0, TextWidthWithANSICodes("\r\n"))
}
