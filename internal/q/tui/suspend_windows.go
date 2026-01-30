//go:build windows

package tui

func suspendSupported() bool {
	return false
}

func (t *TUI) performSuspend() error {
	return nil
}

func (t *TUI) resumeFromSuspend() {}
