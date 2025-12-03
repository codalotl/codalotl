//go:build !windows

package tui

import (
	"os"
	"syscall"
)

func duplicateFile(f *os.File) (*os.File, error) {
	fd := int(f.Fd())
	dup, err := syscall.Dup(fd)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(dup), f.Name()+"-dup"), nil
}
