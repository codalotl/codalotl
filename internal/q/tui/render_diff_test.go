package tui

import (
	"strings"
	"testing"
)

func TestBuildRenderOutputLocked_SkipClearWhenSameCellWidth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		prevLine string
		newLine  string
	}{
		{
			name:     "ascii_same_width",
			prevLine: "aaaa",
			newLine:  "bbbb",
		},
		{
			name:     "wide_rune_same_width",
			prevLine: "界",
			newLine:  "ab",
		},
		{
			name:     "ansi_styled_same_width",
			prevLine: "abcd",
			newLine:  "\x1b[31mabcd\x1b[0m",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tui := &TUI{prevLines: []string{tc.prevLine}}
			out, changed := tui.buildRenderOutputLocked([]string{tc.newLine})
			if !changed {
				t.Fatalf("expected change output")
			}
			if strings.Contains(out, clearLine) {
				t.Fatalf("expected output to skip clearLine (%q) when overwriting same cell width; got %q", clearLine, out)
			}
			if !strings.Contains(out, tc.newLine) {
				t.Fatalf("expected output to contain new line; got %q", out)
			}
		})
	}
}

func TestBuildRenderOutputLocked_ClearWhenCellWidthChanges(t *testing.T) {
	t.Parallel()

	tui := &TUI{prevLines: []string{"abcd"}}
	out, changed := tui.buildRenderOutputLocked([]string{"ab"})
	if !changed {
		t.Fatalf("expected change output")
	}
	if !strings.Contains(out, clearLine) {
		t.Fatalf("expected output to include clearLine (%q) when cell width changes; got %q", clearLine, out)
	}
}
