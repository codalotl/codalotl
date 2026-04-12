//go:build !unix && !windows

package skills

import "os"

type defaultInstallFileLock struct {
	file *os.File
}

func lockDefaultInstallFile(path string) (*defaultInstallFileLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	return &defaultInstallFileLock{file: file}, nil
}

func (l *defaultInstallFileLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}
