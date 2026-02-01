//go:build linux

package clipboard

import "errors"

func selectBackend() (backend, error) {
	// Prefer wl-clipboard when running under Wayland.
	if getenv("WAYLAND_DISPLAY") != "" {
		if _, err := lookPath("wl-copy"); err == nil {
			if _, err := lookPath("wl-paste"); err == nil {
				return cmdBackend{
					pasteCmd:  "wl-paste",
					pasteArgs: []string{"--no-newline"},
					copyCmd:   "wl-copy",
				}, nil
			}
		}
	}

	if _, err := lookPath("xclip"); err == nil {
		return cmdBackend{
			pasteCmd:  "xclip",
			pasteArgs: []string{"-out", "-selection", "clipboard"},
			copyCmd:   "xclip",
			copyArgs:  []string{"-in", "-selection", "clipboard"},
		}, nil
	}

	if _, err := lookPath("xsel"); err == nil {
		return cmdBackend{
			pasteCmd:  "xsel",
			pasteArgs: []string{"--output", "--clipboard"},
			copyCmd:   "xsel",
			copyArgs:  []string{"--input", "--clipboard"},
		}, nil
	}

	return nil, errors.New("no clipboard utility found (install wl-clipboard, xclip, or xsel)")
}
