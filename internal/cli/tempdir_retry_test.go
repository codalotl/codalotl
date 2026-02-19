package cli

import (
	"errors"
	"os"
	"testing"
	"time"
)

func mkdirTempWithRemoveRetry(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		t.Fatalf("MkdirTemp(%q): %v", prefix, err)
	}

	t.Cleanup(func() {
		// Some subprocesses (notably `go list`) can still be finalizing filesystem
		// writes as tests exit. A short retry loop keeps cleanup from being flaky.
		deadline := time.Now().Add(3 * time.Second)
		backoff := 10 * time.Millisecond
		for {
			rmErr := os.RemoveAll(dir)
			if rmErr == nil || errors.Is(rmErr, os.ErrNotExist) {
				return
			}
			if time.Now().After(deadline) {
				t.Errorf("cleanup temp dir %q: %v", dir, rmErr)
				return
			}
			time.Sleep(backoff)
			if backoff < 200*time.Millisecond {
				backoff *= 2
			}
		}
	})

	return dir
}
