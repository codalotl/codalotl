//go:build darwin

package clipboard

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackendUnavailableWhenPbcopyMissing(t *testing.T) {
	resetForTest(t)
	lookPath = func(string) (string, error) {
		return "", errors.New("not found")
	}

	_, err := getBackend()
	require.Error(t, err)
	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorContains(t, err, "missing pbcopy")
}

func TestBackendUnavailableWhenPbpasteMissing(t *testing.T) {
	resetForTest(t)
	lookPath = func(prog string) (string, error) {
		if prog == "pbcopy" {
			return "/usr/bin/pbcopy", nil
		}
		return "", errors.New("not found")
	}

	_, err := getBackend()
	require.Error(t, err)
	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorContains(t, err, "missing pbpaste")
}

func TestAvailableTrueWhenCommandsPresent(t *testing.T) {
	resetForTest(t)
	lookPath = func(prog string) (string, error) {
		return "/usr/bin/" + prog, nil
	}

	require.True(t, Available())
}
