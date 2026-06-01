//go:build unix

package skills

import (
	"os"
	"syscall"
)

// defaultInstallFileLock represents an exclusive advisory lock on the default skill installation lock file.
type defaultInstallFileLock struct {
	file *os.File // The file field is the open lock file whose descriptor holds the advisory lock.
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

// Close releases the advisory installation lock and closes its lock file. A nil receiver or lock with no file is a no-op. Call Close once after acquiring a lock
// with lockDefaultInstallFile. If both unlocking and closing fail, Close returns the unlock error.
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
