package clipboard

import "os/exec"

type cmdBackend struct {
	pasteCmd  string
	pasteArgs []string

	copyCmd  string
	copyArgs []string
}

func (b cmdBackend) read() (string, error) {
	out, err := exec.Command(b.pasteCmd, b.pasteArgs...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

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
