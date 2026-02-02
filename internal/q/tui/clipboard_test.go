package tui

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTUISetClipboard_WritesOSC52(t *testing.T) {
	var buf bytes.Buffer
	tui := &TUI{output: &buf}

	tui.SetClipboard("hello")

	assert.Equal(t, "\x1b]52;c;aGVsbG8=\x07", buf.String())
}

func TestTUISetClipboard_Base64EncodesText(t *testing.T) {
	var buf bytes.Buffer
	tui := &TUI{output: &buf}

	tui.SetClipboard("hello\nworld")

	out := buf.String()
	assert.Equal(t, "\x1b]52;c;aGVsbG8Kd29ybGQ=\x07", out)
	assert.NotContains(t, out, "hello")
	assert.NotContains(t, out, "world")
}

func TestTUISetClipboard_NoOutputAfterStop(t *testing.T) {
	var buf bytes.Buffer
	tui := &TUI{output: &buf, stopping: true}

	tui.SetClipboard("hello")

	assert.Equal(t, "", buf.String())
}

func TestTUISetClipboard_NoPanicOnNilOutput(t *testing.T) {
	tui := &TUI{}
	require.NotPanics(t, func() {
		tui.SetClipboard("hello")
	})
}
