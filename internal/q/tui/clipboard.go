package tui

import (
	"encoding/base64"
	"io"
)

// SetClipboard requests that the terminal set the clipboard contents to text (copy). This is best-effort and may be ignored by the user's terminal.
func (t *TUI) SetClipboard(text string) {
	if t == nil {
		return
	}

	t.mu.Lock()
	if t.stopping {
		t.mu.Unlock()
		return
	}
	out := t.output
	t.mu.Unlock()
	if out == nil {
		return
	}

	// OSC52: ESC ] 52 ; c ; <base64(text)> BEL
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"

	t.renderMu.Lock()
	_, _ = io.WriteString(out, seq)
	t.renderMu.Unlock()
}
