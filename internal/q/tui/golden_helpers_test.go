package tui_test

import (
	"bytes"
	"io"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// requireTestTTY returns a pseudo-terminal hooked up to a background reader so the integration test can exercise the real terminal paths even when the test process
// itself is not attached to a tty.
func requireTestTTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()

	input, output, _ := setupTestTTY(t, true, nil)
	return input, output
}

// requireTestTTYWithPTY works like requireTestTTY but also returns the PTY control endpoint so tests can write keyboard input.
func requireTestTTYWithPTY(t *testing.T) (*os.File, *os.File, *os.File) {
	t.Helper()

	return setupTestTTY(t, false, nil)
}

// requireTestTTYWithCapture works like requireTestTTY but also returns a buffer containing the terminal output.
func requireTestTTYWithCapture(t *testing.T) (*os.File, *os.File, *syncedBuffer) {
	t.Helper()

	buf := &syncedBuffer{}
	input, output, _ := setupTestTTY(t, true, buf)
	return input, output, buf
}

func setupTestTTY(t *testing.T, injectUnblock bool, capture io.Writer) (*os.File, *os.File, *os.File) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("tui integration test requires a TTY")
	}

	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatalf("failed to allocate pseudo terminal: %v", err)
	}
	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
		_ = ptmx.Close()
		_ = tty.Close()
		t.Fatalf("failed to set pty size: %v", err)
	}

	inputFD, err := unix.Dup(int(tty.Fd()))
	if err != nil {
		_ = ptmx.Close()
		_ = tty.Close()
		t.Fatalf("failed to dup tty: %v", err)
	}
	input := os.NewFile(uintptr(inputFD), tty.Name()+"-input")

	drainDone := make(chan struct{})
	drainWriter := io.Writer(io.Discard)
	if capture != nil {
		drainWriter = io.MultiWriter(io.Discard, capture)
	}
	go func() {
		_, _ = io.Copy(drainWriter, ptmx)
		close(drainDone)
	}()

	var unblockTimer *time.Timer
	if injectUnblock {
		unblockTimer = time.AfterFunc(100*time.Millisecond, func() {
			_, _ = ptmx.Write([]byte{'\n'})
		})
	}

	t.Cleanup(func() {
		if unblockTimer != nil {
			unblockTimer.Stop()
		}
		_ = input.Close()
		_ = tty.Close()
		_ = ptmx.Close()
		<-drainDone
	})

	return input, tty, ptmx
}

type syncedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
