//go:build unix

package skills

import (
	"os"
	"syscall"
)

type defaultInstallFileLock struct {
	file *os.File
}

func lockDefaultInstallFile(path string) (*defaultInstallFileLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &defaultInstallFileLock{file: file}, nil
}

func (l *defaultInstallFileLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
