package tui

import "os"

type fdProvider interface {
	Fd() uintptr
}

type statProvider interface {
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
