package termformat

import (
	"os"
	"strconv"
	"strings"
)

// DefaultFBBGColor attempts to detect the terminal's default foreground/background colors. If it cannot be determined, an error occurs, or if we determine that
// we shouldn't use colors, returns NoColor{} as both fg and bg. If the returned colors are not NoColor, they are up-sampled or down-sampled to ColorProfile (ex:
// if the terminal is ColorProfileANSI256, the returned colors will be ANSI256Color).
func DefaultFBBGColor() (fg Color, bg Color) {
	profile, err := GetColorProfile()
	if err != nil || profile == ColorProfileUncolored {
		return NoColor{}, NoColor{}
	}

	fgRGB, bgRGB, ok, err := queryDefaultFBBGColor()
	if err != nil || !ok {
		if fgRGB, bgRGB, ok = parseColorFGBG(os.Getenv("COLORFGBG")); !ok {
			return NoColor{}, NoColor{}
		}
	}

	return convertRGBToProfile(fgRGB, profile), convertRGBToProfile(bgRGB, profile)
}

func convertRGBToProfile(color RGBColor, profile ColorProfile) Color {
	switch profile {
	case ColorProfileANSI:
		return color.ANSIColor()
	case ColorProfileANSI256:
		return color.ANSI256Color()
	case ColorProfileTrueColor:
		return color
	default:
		return NoColor{}
	}
}

func parseColorFGBG(value string) (RGBColor, RGBColor, bool) {
	if !strings.Contains(value, ";") {
		return "", "", false
	}
	parts := strings.Split(value, ";")
	if len(parts) < 2 {
		return "", "", false
	}

	fgIdx, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", "", false
	}
	bgIdx, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return "", "", false
	}

	fg := ANSIColor(fgIdx)
	bg := ANSIColor(bgIdx)
	if !fg.Valid() || !bg.Valid() {
		return "", "", false
	}

	return fg.RGBColor(), bg.RGBColor(), true
}
