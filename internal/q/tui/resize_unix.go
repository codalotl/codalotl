//go:build !windows

package tui

// The startResizeWatcher method is a Unix no-op because resize notifications are delivered through SIGWINCH.
func (t *TUI) startResizeWatcher() {}
