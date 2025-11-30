//go:build windows

package tui

import (
	"os"
	"syscall"
)

func signalBindings() []signalBinding {
	return []signalBinding{
		{sig: os.Interrupt, action: func(t *TUI) { t.Interrupt() }},
		{sig: syscall.SIGTERM, action: func(t *TUI) { t.Quit() }},
	}
}
