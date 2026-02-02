//go:build !darwin && !linux && !windows

package clipboard

import "errors"

func selectBackend() (backend, error) {
	return nil, errors.New("unsupported platform")
}
