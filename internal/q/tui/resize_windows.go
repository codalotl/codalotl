//go:build windows

package tui

import "time"

func (t *TUI) startResizeWatcher() {
	ticker := time.NewTicker(250 * time.Millisecond)
	if !t.registerStopCloser(func() {
		ticker.Stop()
	}) {
		ticker.Stop()
		return
	}

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		for {
			select {
			case <-t.ctx.Done():
				return
			case <-ticker.C:
				t.triggerResizeEvent()
			}
		}
	}()
}
