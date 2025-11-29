//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris || zos

package termformat

import (
	"os"
	"strings"
)

func osColorProfile() (ColorProfile, error) {
	if !stdoutIsTTY() {
		return ColorProfileUncolored, nil
	}

	if os.Getenv("GOOGLE_CLOUD_SHELL") == "true" {
		return ColorProfileTrueColor, nil
	}

	term := strings.ToLower(os.Getenv("TERM"))
	colorTerm := strings.ToLower(os.Getenv("COLORTERM"))

	switch colorTerm {
	case "24bit", "truecolor":
		if strings.HasPrefix(term, "screen") && !strings.EqualFold(os.Getenv("TERM_PROGRAM"), "tmux") {
			return ColorProfileANSI256, nil
		}
		return ColorProfileTrueColor, nil
	case "yes", "true":
		return ColorProfileANSI256, nil
	}

	switch term {
	case "alacritty", "contour", "rio", "wezterm", "xterm-ghostty", "xterm-kitty":
		return ColorProfileTrueColor, nil
	case "linux", "xterm":
		return ColorProfileANSI, nil
	}

	if strings.Contains(term, "256color") {
		return ColorProfileANSI256, nil
	}
	if strings.Contains(term, "color") || strings.Contains(term, "ansi") {
		return ColorProfileANSI, nil
	}

	return ColorProfileUncolored, nil
}
