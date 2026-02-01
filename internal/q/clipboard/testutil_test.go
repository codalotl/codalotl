package clipboard

import (
	"os"
	"os/exec"
	"sync"
	"testing"
)

func resetForTest(t *testing.T) {
	t.Helper()

	backendOnce = sync.Once{}
	backendImpl = nil
	backendErr = nil

	lookPath = exec.LookPath
	getenv = os.Getenv
}
