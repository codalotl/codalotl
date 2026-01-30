//go:build windows

package tui

import "syscall"

func isTTY(r any) bool {
	fd, ok := extractFD(r)
	if !ok {
		return false
	}
	if hasCharDevice(r) {
		return true
	}

	var mode uint32
	err := syscall.GetConsoleMode(syscall.Handle(fd), &mode)
	return err == nil
}
