package clipboard

import (
	"errors"
	"os"
	"os/exec"
	"sync"
)

// ErrUnavailable indicates that the clipboard is not usable on this system (typically because the required OS integration or command-line utilities are missing).
var ErrUnavailable = errors.New("clipboard unavailable")

type backend interface {
	read() (string, error)
	write(string) error
}

var (
	backendOnce sync.Once
	backendImpl backend
	backendErr  error

	lookPath = exec.LookPath
	getenv   = os.Getenv
)

// Read reads from the clipboard and returns the text in it.
func Read() (string, error) {
	b, err := getBackend()
	if err != nil {
		return "", err
	}
	return b.read()
}

// Write writes s to the clipboard.
func Write(s string) error {
	b, err := getBackend()
	if err != nil {
		return err
	}
	return b.write(s)
}

// Available reports whether the clipboard is available on this system.
//
// This is intended as a cheap capability check for gating UI/feature flags.
func Available() bool {
	_, err := getBackend()
	return err == nil
}

func getBackend() (backend, error) {
	backendOnce.Do(func() {
		backendImpl, backendErr = selectBackend()
		if backendErr != nil {
			backendErr = errors.Join(ErrUnavailable, backendErr)
		}
	})
	return backendImpl, backendErr
}
