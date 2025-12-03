//go:build windows

package tui

import (
	"io"
	"os"
	"syscall"
)

const enableVirtualTerminalProcessing = 0x0004

func enableVirtualTerminal(out io.Writer) (func() error, error) {
	file, ok := out.(*os.File)
	if !ok || file == nil {
		return nil, nil
	}

	handle := syscall.Handle(file.Fd())
	var mode uint32
	if err := syscall.GetConsoleMode(handle, &mode); err != nil {
		return nil, err
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return func() error { return nil }, nil
	}

	if err := syscall.SetConsoleMode(handle, mode|enableVirtualTerminalProcessing); err != nil {
		return nil, err
	}

	return func() error {
		return syscall.SetConsoleMode(handle, mode)
	}, nil
}
