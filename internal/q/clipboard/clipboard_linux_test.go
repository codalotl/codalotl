//go:build linux

package clipboard

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectBackendPrefersWaylandWhenAvailable(t *testing.T) {
	resetForTest(t)
	getenv = func(key string) string {
		if key == "WAYLAND_DISPLAY" {
			return "1"
		}
		return ""
	}
	lookPath = func(prog string) (string, error) {
		switch prog {
		case "wl-copy", "wl-paste":
			return "/bin/" + prog, nil
		default:
			return "", errors.New("not found")
		}
	}

	b, err := selectBackend()
	require.NoError(t, err)

	cb, ok := b.(cmdBackend)
	require.True(t, ok)
	require.Equal(t, "wl-paste", cb.pasteCmd)
	require.Equal(t, []string{"--no-newline"}, cb.pasteArgs)
	require.Equal(t, "wl-copy", cb.copyCmd)
	require.Empty(t, cb.copyArgs)
}

func TestSelectBackendFallsBackToXclipWhenWaylandMissingTools(t *testing.T) {
	resetForTest(t)
	getenv = func(key string) string {
		if key == "WAYLAND_DISPLAY" {
			return "1"
		}
		return ""
	}
	lookPath = func(prog string) (string, error) {
		switch prog {
		case "wl-copy":
			return "/bin/" + prog, nil
		case "xclip":
			return "/bin/" + prog, nil
		default:
			return "", errors.New("not found")
		}
	}

	b, err := selectBackend()
	require.NoError(t, err)

	cb, ok := b.(cmdBackend)
	require.True(t, ok)
	require.Equal(t, "xclip", cb.pasteCmd)
	require.Equal(t, []string{"-out", "-selection", "clipboard"}, cb.pasteArgs)
	require.Equal(t, "xclip", cb.copyCmd)
	require.Equal(t, []string{"-in", "-selection", "clipboard"}, cb.copyArgs)
}

func TestSelectBackendUsesXselIfXclipMissing(t *testing.T) {
	resetForTest(t)
	lookPath = func(prog string) (string, error) {
		if prog == "xsel" {
			return "/bin/" + prog, nil
		}
		return "", errors.New("not found")
	}

	b, err := selectBackend()
	require.NoError(t, err)

	cb, ok := b.(cmdBackend)
	require.True(t, ok)
	require.Equal(t, "xsel", cb.pasteCmd)
	require.Equal(t, []string{"--output", "--clipboard"}, cb.pasteArgs)
	require.Equal(t, "xsel", cb.copyCmd)
	require.Equal(t, []string{"--input", "--clipboard"}, cb.copyArgs)
}

func TestBackendUnavailableWhenNoTools(t *testing.T) {
	resetForTest(t)
	lookPath = func(string) (string, error) {
		return "", errors.New("not found")
	}

	_, err := getBackend()
	require.Error(t, err)
	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorContains(t, err, "no clipboard utility found")
}

func TestAvailableTrueWhenXclipPresent(t *testing.T) {
	resetForTest(t)
	lookPath = func(prog string) (string, error) {
		if prog == "xclip" {
			return "/bin/" + prog, nil
		}
		return "", errors.New("not found")
	}

	require.True(t, Available())
}
