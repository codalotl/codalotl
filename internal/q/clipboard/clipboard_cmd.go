package clipboard

import "os/exec"

// A cmdBackend implements backend by using external paste and copy commands.
type cmdBackend struct {
	pasteCmd  string   // It names the executable used to read clipboard text.
	pasteArgs []string // It supplies arguments to pasteCmd.
	copyCmd   string   // It names the executable used to write clipboard text.
	copyArgs  []string // It supplies arguments to copyCmd.
}

// The read method runs the configured paste command and returns its standard output as clipboard text. It returns an error if the command cannot run or exits unsuccessfully.
func (b cmdBackend) read() (string, error) {
	out, err := exec.Command(b.pasteCmd, b.pasteArgs...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// The write method runs the configured copy command with s on standard input. It returns an error if starting the command, sending input, or waiting for completion
// fails.
func (b cmdBackend) write(s string) error {
	cmd := exec.Command(b.copyCmd, b.copyArgs...)

	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if _, err := in.Write([]byte(s)); err != nil {
		return err
	}
	if err := in.Close(); err != nil {
		return err
	}
	return cmd.Wait()
}
