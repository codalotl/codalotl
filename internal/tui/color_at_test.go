package tui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/q/termformat"
	"github.com/codalotl/codalotl/internal/q/uni"

	"github.com/stretchr/testify/require"
)

// colorAt returns the effective foreground or background color active at cell (targetX, targetY) within the ANSI-styled string str.
func colorAt(str string, targetX, targetY int, bg bool) termformat.Color {
	if targetX < 0 || targetY < 0 {
		return nil
	}

	state := ansiColorState{}
	curX, curY := 0, 0

	for i := 0; i < len(str); {
		switch str[i] {
		case '\x1b':
			consumed := consumeSGR(str[i:], &state)
			if consumed <= 0 {
				i++
				continue
			}
			i += consumed
		case '\n':
			if curY == targetY && targetX >= curX {
				return nil
			}
			curY++
			curX = 0
			i++
			if curY > targetY {
				return nil
			}
		case '\r':
			i++
		default:
			next := i
			for next < len(str) {
				if str[next] == '\x1b' || str[next] == '\n' || str[next] == '\r' {
					break
				}
				next++
			}
			segment := str[i:next]
			iter := uni.NewGraphemeIterator(segment, nil)
			for iter.Next() {
				width := iter.TextWidth()
				if width <= 0 {
					continue
				}
				for step := 0; step < width; step++ {
					if curX == targetX && curY == targetY {
						if bg {
							return state.bg
						}
						return state.fg
					}
					curX++
				}
			}
			i = next
		}
	}

	return nil
}

type ansiColorState struct {
	fg termformat.Color
	bg termformat.Color
}

func consumeSGR(seq string, state *ansiColorState) int {
	if len(seq) < 2 || seq[0] != '\x1b' || seq[1] != '[' {
		return 0
	}
	end := 2
	for end < len(seq) && seq[end] != 'm' {
		end++
	}
	if end >= len(seq) || seq[end] != 'm' {
		return 0
	}
	if params, ok := parseSGRParams(seq[2:end]); ok {
		applySGRParams(state, params)
	}
	return end + 1
}

func parseSGRParams(content string) ([]int, bool) {
	if content == "" {
		return []int{0}, true
	}
	parts := strings.Split(content, ";")
	params := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			params = append(params, 0)
			continue
		}
		val, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		params = append(params, val)
	}
	return params, true
}

func applySGRParams(state *ansiColorState, params []int) {
	if len(params) == 0 {
		return
	}
	for i := 0; i < len(params); i++ {
		p := params[i]
		switch {
		case p == 0:
			state.fg = nil
			state.bg = nil
		case p == 39:
			state.fg = nil
		case p == 49:
			state.bg = nil
		case isForegroundCode(p):
			if c := ansiColorFromCode(p); c != nil {
				state.fg = c
			}
		case isBackgroundCode(p):
			if c := ansiColorFromCode(p); c != nil {
				state.bg = c
			}
		case p == 38:
			if color, advanced, ok := parseExtendedColor(params, i); ok {
				state.fg = color
				i = advanced
			}
		case p == 48:
			if color, advanced, ok := parseExtendedColor(params, i); ok {
				state.bg = color
				i = advanced
			}
		}
	}
}

func isForegroundCode(p int) bool {
	return (p >= 30 && p <= 37) || (p >= 90 && p <= 97)
}

func isBackgroundCode(p int) bool {
	return (p >= 40 && p <= 47) || (p >= 100 && p <= 107)
}

func ansiColorFromCode(p int) termformat.Color {
	switch {
	case p >= 30 && p <= 37:
		return termformat.ANSIColor(p - 30)
	case p >= 90 && p <= 97:
		return termformat.ANSIColor(p - 90 + 8)
	case p >= 40 && p <= 47:
		return termformat.ANSIColor(p - 40)
	case p >= 100 && p <= 107:
		return termformat.ANSIColor(p - 100 + 8)
	default:
		return nil
	}
}

func parseExtendedColor(params []int, idx int) (termformat.Color, int, bool) {
	if idx+1 >= len(params) {
		return nil, idx, false
	}
	mode := params[idx+1]
	switch mode {
	case 5:
		if idx+2 >= len(params) {
			return nil, idx, false
		}
		return termformat.ANSI256Color(params[idx+2]), idx + 2, true
	case 2:
		if idx+4 >= len(params) {
			return nil, idx, false
		}
		r := clampColorComponent(params[idx+2])
		g := clampColorComponent(params[idx+3])
		b := clampColorComponent(params[idx+4])
		return termformat.NewRGBColor(r, g, b), idx + 4, true
	default:
		return nil, idx, false
	}
}

func clampColorComponent(value int) uint8 {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return uint8(value)
}

func requireColorEqual(t *testing.T, expected, actual termformat.Color) {
	t.Helper()
	require.Truef(t, colorsEqual(expected, actual), "expected color %s, got %s", colorString(expected), colorString(actual))

}

func colorsEqual(a, b termformat.Color) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	ar, ag, ab := a.RGB8()
	br, bg, bb := b.RGB8()
	return ar == br && ag == bg && ab == bb && fmt.Sprintf("%T", a) == fmt.Sprintf("%T", b)
}

func colorString(c termformat.Color) string {
	if c == nil {
		return "<nil>"
	}
	r, g, b := c.RGB8()
	return fmt.Sprintf("%T(0x%02x,0x%02x,0x%02x)", c, r, g, b)
}
