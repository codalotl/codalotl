//go:build !windows

package termformat

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func queryDefaultFBBGColor() (RGBColor, RGBColor, bool, error) {
	stdout := os.Stdout
	if !term.IsTerminal(int(stdout.Fd())) {
		return "", "", false, nil
	}
	termEnv := os.Getenv("TERM")
	if strings.HasPrefix(termEnv, "screen") || strings.HasPrefix(termEnv, "tmux") || strings.HasPrefix(termEnv, "dumb") {
		return "", "", false, nil
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENXIO) {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	fd := int(tty.Fd())

	state, err := term.MakeRaw(fd)
	if err != nil {
		tty.Close()
		return "", "", false, err
	}
	defer func() {
		_ = term.Restore(fd, state)
		_ = tty.Close()
	}()

	if _, err := tty.WriteString("\x1b]10;?\x07\x1b]11;?\x07"); err != nil {
		return "", "", false, err
	}

	if err := unix.SetNonblock(fd, true); err != nil {
		return "", "", false, err
	}

	reader := bufio.NewReader(tty)
	deadline := time.Now().Add(200 * time.Millisecond)
	var buffer []byte
	var fg, bg RGBColor
	var haveFG, haveBG bool

	for time.Now().Before(deadline) {
		chunk := make([]byte, 128)
		n, readErr := reader.Read(chunk)
		if n > 0 {
			buffer = append(buffer, chunk[:n]...)
			if !haveFG {
				if color, ok := parseOSCColor(buffer, "10"); ok {
					fg, haveFG = color, true
				}
			}
			if !haveBG {
				if color, ok := parseOSCColor(buffer, "11"); ok {
					bg, haveBG = color, true
				}
			}
			if haveFG && haveBG {
				return fg, bg, true, nil
			}
			continue
		}

		if readErr != nil {
			if errors.Is(readErr, syscall.EAGAIN) || errors.Is(readErr, syscall.EWOULDBLOCK) {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			return "", "", false, readErr
		}
	}

	if !haveFG {
		if color, ok := parseOSCColor(buffer, "10"); ok {
			fg, haveFG = color, true
		}
	}
	if !haveBG {
		if color, ok := parseOSCColor(buffer, "11"); ok {
			bg, haveBG = color, true
		}
	}
	if haveFG && haveBG {
		return fg, bg, true, nil
	}

	return "", "", false, nil
}

func parseOSCColor(buffer []byte, code string) (RGBColor, bool) {
	text := string(buffer)
	prefix := "\x1b]" + code + ";"
	start := strings.LastIndex(text, prefix)
	if start == -1 {
		return "", false
	}
	payload := text[start+len(prefix):]
	end := terminationIndex(payload)
	if end < 0 {
		return "", false
	}
	payload = payload[:end]
	r, g, b, ok := parsePayload(payload)
	if !ok {
		return "", false
	}
	return NewRGBColor(r, g, b), true
}

func parsePayload(payload string) (r, g, b uint8, ok bool) {
	if payload == "" || payload == "?" {
		return 0, 0, 0, false
	}
	model, values, found := strings.Cut(payload, ":")
	if !found || (model != "rgb" && model != "rgba") {
		return 0, 0, 0, false
	}
	var comps [3]uint8
	for i := 0; i < 3; i++ {
		component := values
		if i < 2 {
			var more bool
			component, values, more = strings.Cut(values, "/")
			if !more {
				return 0, 0, 0, false
			}
		}
		v, ok := parseHexComponentPayload(component)
		if !ok {
			return 0, 0, 0, false
		}
		comps[i] = v
	}
	return comps[0], comps[1], comps[2], true
}

func parseHexComponentPayload(component string) (uint8, bool) {
	component = strings.TrimSpace(component)
	if component == "" {
		return 0, false
	}
	bits := len(component) * 4
	if bits == 0 || bits > 64 {
		return 0, false
	}
	value, err := strconv.ParseUint(component, 16, bits)
	if err != nil {
		return 0, false
	}
	var max uint64
	if bits == 64 {
		max = ^uint64(0)
	} else {
		max = (uint64(1) << bits) - 1
	}
	return uint8((value*255 + max/2) / max), true
}

func terminationIndex(payload string) int {
	end := -1
	if idx := strings.IndexByte(payload, '\x07'); idx >= 0 {
		end = idx
	}
	if idx := strings.Index(payload, "\x1b\\"); idx >= 0 {
		if end == -1 || idx < end {
			end = idx
		}
	}
	return end
}
