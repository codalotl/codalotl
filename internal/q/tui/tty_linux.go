//go:build linux || android

package tui

import (
	"syscall"
	"unsafe"
)

func isTTY(r any) bool {
	fd, ok := extractFD(r)
	if !ok {
		return false
	}
	if hasCharDevice(r) {
		return true
	}

	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&termios)))
	return errno == 0
}
