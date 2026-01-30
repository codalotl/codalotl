//go:build !windows

package tui

import "syscall"

func signalBindings() []signalBinding {
	return []signalBinding{
		{sig: syscall.SIGINT, action: func(t *TUI) { t.Interrupt() }},
		{sig: syscall.SIGTERM, action: func(t *TUI) { t.Quit() }},
		{sig: syscall.SIGWINCH, action: func(t *TUI) { t.handleResizeSignal() }},
		{sig: syscall.SIGTSTP, action: func(t *TUI) { t.enqueueSuspend() }},
		{sig: syscall.SIGCONT, action: func(t *TUI) { t.resumeFromSuspend() }},
	}
}
