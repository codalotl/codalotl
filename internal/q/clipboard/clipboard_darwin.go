//go:build darwin

package clipboard

import "errors"

func selectBackend() (backend, error) {
	if _, err := lookPath("pbcopy"); err != nil {
		return nil, errors.New("missing pbcopy")
	}
	if _, err := lookPath("pbpaste"); err != nil {
		return nil, errors.New("missing pbpaste")
	}

	return cmdBackend{
		pasteCmd: "pbpaste",
		copyCmd:  "pbcopy",
	}, nil
}
