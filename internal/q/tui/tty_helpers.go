package tui

import "os"

// An fdProvider exposes an OS file descriptor.
type fdProvider interface {
	// Fd returns the underlying OS file descriptor.
	Fd() uintptr
}

// A statProvider exposes file metadata.
type statProvider interface {
	// Stat returns metadata for the underlying file or stream.
	Stat() (os.FileInfo, error)
}

func extractFD(r any) (int, bool) {
	fp, ok := r.(fdProvider)
	if !ok {
		return -1, false
	}
	return int(fp.Fd()), true
}

func hasCharDevice(r any) bool {
	sp, ok := r.(statProvider)
	if !ok {
		return false
	}
	info, err := sp.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
