package termformat

import (
	"fmt"
	"math"
	"strconv"
)

// Color is an interface that can give you RGB values (implements image/color.Color) and also generate ANSI sequences for foreground/background colors.
type Color interface {
	// RGBA implements image/color.Color.
	RGBA() (r, g, b, a uint32)

	// RGB8 returns RGB from RGBA, shifted right 8 bits, dropping A.
	RGB8() (r, g, b uint8)

	// ANSISequence returns the ANSI sequence for the foreground(bg=false) or background(bg=true) color. Ex: "\033[31m".
	ANSISequence(bg bool) string
}

type NoColor struct{}

// String implements fmt.Stringer.
func (NoColor) String() string {
	return "none"
}

// ANSIColor is a color (0-15) as defined by the ANSI Standard.
type ANSIColor int

// String implements fmt.Stringer.
func (ac ANSIColor) String() string {
	if !ac.Valid() {
		return "ansi:invalid"
	}
	return fmt.Sprintf("ansi:%d", int(ac))
}

// ANSI256Color is a color (0-255) as defined by the ANSI Standard.
type ANSI256Color int

// String implements fmt.Stringer.
func (ac ANSI256Color) String() string {
	if !ac.Valid() {
		return "ansi256:invalid"
	}
	return fmt.Sprintf("ansi256:%d", int(ac))
}

// RGBColor is a hex-encoded color. Ex: "#abcdef".
type RGBColor string

// String implements fmt.Stringer.
func (rc RGBColor) String() string {
	return string(rc)
}

const invalidRGBColor RGBColor = "#000000"

var (
	_ Color = NoColor{}
	_ Color = ANSIColor(0)
	_ Color = ANSI256Color(0)
	_ Color = RGBColor(invalidRGBColor)
)

type labColor struct {
	L float64
	A float64
	B float64
}

var ansiLab = buildANSILab()

func buildANSILab() []labColor {
	labs := make([]labColor, len(ansiHex))
	for i, hex := range ansiHex {
		lab, ok := RGBColor(hex).lab()
		if !ok {
			continue
		}
		labs[i] = lab
	}
	return labs
}

// RGBA implements image/color.Color.
func (NoColor) RGBA() (r, g, b, a uint32) {
	return 0, 0, 0, 0 // TODO: fix a
}

// RGB8 returns zeroed RGB values.
func (NoColor) RGB8() (r, g, b uint8) {
	return 0, 0, 0
}

// RGBA implements image/color.Color.
func (NoColor) ANSISequence(bg bool) string {
	return ""
}

func NewRGBColor(r, g, b uint8) RGBColor {
	return RGBColor(fmt.Sprintf("#%02x%02x%02x", r, g, b))
}

// RGBA implements image/color.Color.
func (ac ANSIColor) RGBA() (r, g, b, a uint32) {
	return ac.RGBColor().RGBA()
}

// RGB8 returns 8-bit RGB values.
func (ac ANSIColor) RGB8() (r, g, b uint8) {
	r32, g32, b32, _ := ac.RGBA()
	return uint8(r32 >> 8), uint8(g32 >> 8), uint8(b32 >> 8)
}

// ANSISequence returns the ANSI escape sequence for an ANSIColor.
func (ac ANSIColor) ANSISequence(bg bool) string {
	if !ac.Valid() {
		return ""
	}

	codeBase := 30
	if bg {
		codeBase = 40
	}

	ci := int(ac)
	switch {
	case ci < 8:
		return fmt.Sprintf("\x1b[%dm", codeBase+ci)
	default:
		if bg {
			return fmt.Sprintf("\x1b[%dm", 100+(ci-8))
		}
		return fmt.Sprintf("\x1b[%dm", 90+(ci-8))
	}
}

// RGBA implements image/color.Color.
func (ac ANSI256Color) RGBA() (r, g, b, a uint32) {
	return ac.RGBColor().RGBA()
}

// RGB8 returns 8-bit RGB values.
func (ac ANSI256Color) RGB8() (r, g, b uint8) {
	r32, g32, b32, _ := ac.RGBA()
	return uint8(r32 >> 8), uint8(g32 >> 8), uint8(b32 >> 8)
}

// ANSISequence returns the ANSI escape sequence for an ANSI256Color.
func (ac ANSI256Color) ANSISequence(bg bool) string {
	if !ac.Valid() {
		return ""
	}

	if bg {
		return fmt.Sprintf("\x1b[48;5;%dm", int(ac))
	}
	return fmt.Sprintf("\x1b[38;5;%dm", int(ac))
}

// RGBA implements image/color.Color.
func (rc RGBColor) RGBA() (r, g, b, a uint32) {
	r8, g8, b8, ok := rc.rgb()
	if !ok {
		return 0, 0, 0, 0xffff
	}
	return uint32(r8) * 0x101, uint32(g8) * 0x101, uint32(b8) * 0x101, 0xffff
}

// RGB8 returns 8-bit RGB values.
func (rc RGBColor) RGB8() (r, g, b uint8) {
	r, g, b, _ = rc.rgb()
	return r, g, b
}

// ANSISequence returns the ANSI escape sequence for the RGB color (true color).
func (rc RGBColor) ANSISequence(bg bool) string {
	r8, g8, b8, ok := rc.rgb()
	if !ok {
		return ""
	}
	if bg {
		return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r8, g8, b8)
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r8, g8, b8)
}

// RGBColor turns an RGBColor (true color) version of ac. Invalid colors return "#000000".
func (ac ANSIColor) RGBColor() RGBColor {
	if !ac.Valid() {
		return invalidRGBColor
	}
	return ANSI256Color(ac).RGBColor()
}

// RGBColor turns an RGBColor (true color) version of ac. Invalid colors return "#000000".
func (ac ANSI256Color) RGBColor() RGBColor {
	if !ac.Valid() || int(ac) >= len(ansiHex) {
		return invalidRGBColor
	}
	return RGBColor(ansiHex[int(ac)])
}

// ANSI256Color returns the closest color in the 256-color space using CIELAB ΔE. Invalid colors return 0.
func (rc RGBColor) ANSI256Color() ANSI256Color {
	lab, ok := rc.lab()
	if !ok {
		return 0
	}

	minDelta := math.MaxFloat64
	best := 0
	for i, candidate := range ansiLab {
		d := deltaE(lab, candidate)
		if d < minDelta {
			minDelta = d
			best = i
		}
	}
	return ANSI256Color(best)
}

// ANSIColor returns the closest color in the 16-color space using CIELAB ΔE. Invalid colors return 0.
func (rc RGBColor) ANSIColor() ANSIColor {
	lab, ok := rc.lab()
	if !ok {
		return 0
	}

	minDelta := math.MaxFloat64
	best := 0
	for i := 0; i < 16 && i < len(ansiLab); i++ {
		d := deltaE(lab, ansiLab[i])
		if d < minDelta {
			minDelta = d
			best = i
		}
	}
	return ANSIColor(best)
}

// Valid returns true if c is valid (0-15).
func (c ANSIColor) Valid() bool {
	return c >= 0 && c < 16
}

// Valid returns true if c is valid (0-255).
func (c ANSI256Color) Valid() bool {
	return c >= 0 && c < 256
}

// Valid returns true if c is valid (of the form, "#abcdef").
func (c RGBColor) Valid() bool {
	s := string(c)
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isHexDigit(s[i]) {
			return false
		}
	}
	return true
}

func (rc RGBColor) rgb() (r, g, b uint8, ok bool) {
	if !rc.Valid() {
		return 0, 0, 0, false
	}

	s := string(rc)
	rv, err := parseHexComponent(s[1:3])
	if err != nil {
		return 0, 0, 0, false
	}
	gv, err := parseHexComponent(s[3:5])
	if err != nil {
		return 0, 0, 0, false
	}
	bv, err := parseHexComponent(s[5:7])
	if err != nil {
		return 0, 0, 0, false
	}

	return rv, gv, bv, true
}

func (rc RGBColor) lab() (labColor, bool) {
	r, g, b, ok := rc.rgb()
	if !ok {
		return labColor{}, false
	}
	return rgbToLab(r, g, b), true
}

func parseHexComponent(component string) (uint8, error) {
	val, err := strconv.ParseUint(component, 16, 8)
	if err != nil {
		return 0, err
	}
	return uint8(val), nil
}

func isHexDigit(ch byte) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

func rgbToLab(r, g, b uint8) labColor {
	R := srgbToLinear(float64(r) / 255.0)
	G := srgbToLinear(float64(g) / 255.0)
	B := srgbToLinear(float64(b) / 255.0)

	x := R*0.4124564 + G*0.3575761 + B*0.1804375
	y := R*0.2126729 + G*0.7151522 + B*0.0721750
	z := R*0.0193339 + G*0.1191920 + B*0.9503041

	xr := x / 0.95047
	yr := y / 1.00000
	zr := z / 1.08883

	fx := labPivot(xr)
	fy := labPivot(yr)
	fz := labPivot(zr)

	return labColor{
		L: 116*fy - 16,
		A: 500 * (fx - fy),
		B: 200 * (fy - fz),
	}
}

func srgbToLinear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func labPivot(v float64) float64 {
	if v > 0.008856 {
		return math.Cbrt(v)
	}
	return (7.787 * v) + (16.0 / 116.0)
}

func deltaE(c1, c2 labColor) float64 {
	dL := c1.L - c2.L
	dA := c1.A - c2.A
	dB := c1.B - c2.B
	return math.Sqrt(dL*dL + dA*dA + dB*dB)
}
