package terminal

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func Start(name string, args ...string) (*os.File, error) {
	cmd := exec.Command(name, args...)
	return pty.Start(cmd)
}
