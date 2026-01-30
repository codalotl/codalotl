package termformat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConvertUncolored(t *testing.T) {
	got := ColorProfileUncolored.Convert(ANSIRed)
	require.Equal(t, NoColor{}, got)
}

func TestConvertInvalidProfile(t *testing.T) {
	color := RGBColor("#112233")
	got := ColorProfile("bad").Convert(color)
	require.Equal(t, color, got)
}

func TestConvertFromRGB(t *testing.T) {
	got := ColorProfileANSI.Convert(RGBColor("#ff0000"))
	require.Equal(t, ANSIColor(ANSIBrightRed), got)
}

func TestConvertANSI256ToTrueColor(t *testing.T) {
	got := ColorProfileTrueColor.Convert(ANSI256Color(45))
	require.Equal(t, RGBColor("#00d7ff"), got)
}

func TestConvertANSIToANSI256(t *testing.T) {
	got := ColorProfileANSI256.Convert(ANSIBrightBlue)
	require.Equal(t, ANSI256Color(12), got)
}

func TestConvertNoColorPassthrough(t *testing.T) {
	got := ColorProfileANSI256.Convert(NoColor{})
	require.Equal(t, NoColor{}, got)
}
