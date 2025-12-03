//go:build !windows

package tui

import "io"

func enableVirtualTerminal(io.Writer) (func() error, error) {
	return nil, nil
}
