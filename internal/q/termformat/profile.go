package termformat

import (
	"os"

	"golang.org/x/term"
)

// ColorProfile is a color profile: None, ANSI, ANSI256, or TrueColor.
type ColorProfile string

const (
	ColorProfileTrueColor ColorProfile = "true_color"
	ColorProfileANSI256   ColorProfile = "ansi256"
	ColorProfileANSI      ColorProfile = "ansi16"
	ColorProfileUncolored ColorProfile = "uncolored"
)

func GetColorProfile() (ColorProfile, error) {
	if envNoColor() {
		return ColorProfileUncolored, nil
	}

	profile, err := osColorProfile()
	if err != nil {
		return ColorProfileUncolored, err
	}

	if profile == ColorProfileUncolored && cliColorForced() {
		return ColorProfileANSI, nil
	}

	return profile, nil
}

// Convert converts c to the ColorProfile p. Converting from higher->lower colors uses some form of closest color matching. ColorProfileUncolored results in NoColor.
// An invalid profile returns c unchanged.
func (p ColorProfile) Convert(c Color) Color {
	if c == nil {
		return nil
	}

	if p == ColorProfileUncolored {
		return NoColor{}
	}

	// Return c unchanged if invalid profile:
	switch p {
	case ColorProfileANSI, ColorProfileANSI256, ColorProfileTrueColor:
	default:
		return c
	}

	switch v := c.(type) {
	case NoColor:
		return v
	case ANSIColor:
		base := v.RGBColor()
		switch p {
		case ColorProfileANSI:
			return v
		case ColorProfileANSI256:
			return base.ANSI256Color()
		case ColorProfileTrueColor:
			return base
		}
	case ANSI256Color:
		base := v.RGBColor()
		switch p {
		case ColorProfileANSI:
			return base.ANSIColor()
		case ColorProfileANSI256:
			return v
		case ColorProfileTrueColor:
			return base
		}
	case RGBColor:
		switch p {
		case ColorProfileANSI:
			return v.ANSIColor()
		case ColorProfileANSI256:
			return v.ANSI256Color()
		case ColorProfileTrueColor:
			return v
		}
	default:
		r, g, b := c.RGB8()
		rgb := NewRGBColor(r, g, b)
		switch p {
		case ColorProfileANSI:
			return rgb.ANSIColor()
		case ColorProfileANSI256:
			return rgb.ANSI256Color()
		case ColorProfileTrueColor:
			return rgb
		}
	}

	return c
}

func envNoColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return true
	}
	return os.Getenv("CLICOLOR") == "0" && !cliColorForced()
}

func cliColorForced() bool {
	forced := os.Getenv("CLICOLOR_FORCE")
	if forced == "" {
		return false
	}
	return forced != "0"
}

func stdoutIsTTY() bool {
	if os.Getenv("CI") != "" {
		return false
	}
	fd := os.Stdout.Fd()
	return term.IsTerminal(int(fd))
}
