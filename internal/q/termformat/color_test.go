package termformat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRGBColor(t *testing.T) {
	require.Equal(t, RGBColor("#ff0080"), NewRGBColor(0xff, 0x00, 0x80))
}

func TestRGBColorValid(t *testing.T) {
	tests := []struct {
		name  string
		color RGBColor
		valid bool
	}{
		{"valid lowercase", RGBColor("#abcdef"), true},
		{"valid uppercase", RGBColor("#ABCDEF"), true},
		{"missing hash", RGBColor("abcdef"), false},
		{"short string", RGBColor("#abc"), false},
		{"invalid chars", RGBColor("#abcdex"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.valid, tt.color.Valid())
		})
	}
}

func TestRGBColorRGBA(t *testing.T) {
	r, g, b, a := RGBColor("#123456").RGBA()
	require.Equal(t, uint32(0x12)*0x101, r)
	require.Equal(t, uint32(0x34)*0x101, g)
	require.Equal(t, uint32(0x56)*0x101, b)
	require.Equal(t, uint32(0xffff), a)

	r, g, b, a = RGBColor("bad").RGBA()
	require.Equal(t, uint32(0), r)
	require.Equal(t, uint32(0), g)
	require.Equal(t, uint32(0), b)
	require.Equal(t, uint32(0xffff), a)
}

func TestANSIColorRGBAndSequence(t *testing.T) {
	require.Equal(t, RGBColor("#800000"), ANSIRed.RGBColor())
	require.Equal(t, invalidRGBColor, ANSIColor(99).RGBColor())

	require.Equal(t, "\x1b[31m", ANSIRed.ANSISequence(false))
	require.Equal(t, "\x1b[101m", ANSIBrightRed.ANSISequence(true))
	require.Equal(t, "", ANSIColor(99).ANSISequence(false))
}

func TestANSI256ColorRGBAndSequence(t *testing.T) {
	require.Equal(t, RGBColor("#00d7ff"), ANSI256Color(45).RGBColor())
	require.Equal(t, invalidRGBColor, ANSI256Color(500).RGBColor())

	require.Equal(t, "\x1b[38;5;45m", ANSI256Color(45).ANSISequence(false))
	require.Equal(t, "\x1b[48;5;45m", ANSI256Color(45).ANSISequence(true))
	require.Equal(t, "", ANSI256Color(-1).ANSISequence(false))
}

func TestRGBColorANSISequences(t *testing.T) {
	require.Equal(t, "\x1b[38;2;18;52;86m", RGBColor("#123456").ANSISequence(false))
	require.Equal(t, "\x1b[48;2;18;52;86m", RGBColor("#123456").ANSISequence(true))
	require.Equal(t, "", RGBColor("bad").ANSISequence(false))
}

func TestRGBToANSIConversion(t *testing.T) {
	require.Equal(t, ANSIColor(ANSIBrightRed), RGBColor("#ff0000").ANSIColor())
	require.Equal(t, ANSI256Color(45), RGBColor("#00d7ff").ANSI256Color())
	require.Equal(t, ANSIColor(0), RGBColor("bad").ANSIColor())
	require.Equal(t, ANSI256Color(0), RGBColor("bad").ANSI256Color())
}
