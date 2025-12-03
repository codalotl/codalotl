//go:build windows

package tui

import (
	"os"
	"syscall"
)

func duplicateFile(f *os.File) (*os.File, error) {
	handle := syscall.Handle(f.Fd())
	currentProcess, err := syscall.GetCurrentProcess()
	if err != nil {
		return nil, err
	}
	var dup syscall.Handle
	err = syscall.DuplicateHandle(
		currentProcess,
		handle,
		currentProcess,
		&dup,
		0,
		false,
		syscall.DUPLICATE_SAME_ACCESS,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(dup), f.Name()+"-dup"), nil
}
