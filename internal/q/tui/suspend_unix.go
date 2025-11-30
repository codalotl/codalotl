//go:build !windows

package tui

import "syscall"

func suspendSupported() bool {
	return true
}

func (t *TUI) performSuspend() error {
	t.suspendMu.Lock()
	if t.suspended {
		t.suspendMu.Unlock()
		return nil
	}
	t.suspended = true
	t.suspendMu.Unlock()

	if t.term != nil {
		if err := t.term.Exit(); err != nil {
			return err
		}
	}
	return syscall.Kill(0, syscall.SIGTSTP)
}

func (t *TUI) resumeFromSuspend() {
	t.suspendMu.Lock()
	if !t.suspended {
		t.suspendMu.Unlock()
		return
	}
	t.suspended = false
	t.suspendMu.Unlock()

	if err := t.enterTerminal(); err != nil {
		t.stop(err)
		return
	}
	t.triggerResizeEvent()
	t.Send(SigResumeEvent{})
}
