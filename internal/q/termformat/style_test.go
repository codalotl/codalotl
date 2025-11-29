package termformat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpeningControlCodes(t *testing.T) {
	tests := []struct {
		name  string
		style Style
		want  string
	}{
		{
			name:  "empty_style",
			style: Style{},
			want:  "",
		},
		{
			name: "style_flags_only",
			style: Style{
				Bold:      StyleSetOn,
				Underline: StyleSetOn,
				Italic:    StyleSetOff,
			},
			want: "\x1b[1;4m",
		},
		{
			name: "ignores_nocolor",
			style: Style{
				Foreground: NoColor{},
				Background: NoColor{},
				Bold:       StyleSetOn,
			},
			want: "\x1b[1m",
		},
		{
			name: "colors_only",
			style: Style{
				Foreground: ANSIRed,
				Background: ANSI256Color(42),
			},
			want: "\x1b[31m\x1b[48;5;42m",
		},
		{
			name: "styles_and_colors",
			style: Style{
				Bold:       StyleSetOn,
				Reverse:    StyleSetOn,
				Foreground: NewRGBColor(1, 2, 3),
			},
			want: "\x1b[1;7m\x1b[38;2;1;2;3m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.style.OpeningControlCodes())
		})
	}
}

func TestStyleWrapMulti(t *testing.T) {
	text := "hi"
	style1 := Style{Bold: StyleSetOn}
	style2 := Style{Italic: StyleSetOn}
	result := style1.Wrap(style2.Wrap(text))
	assert.Equal(t, "\x1b[1m\x1b[3mhi\x1b[0m", result) // one reset
}

func TestStyleWrap(t *testing.T) {
	tests := []struct {
		name  string
		style Style
		input string
		want  string
	}{
		{
			name: "styles_applied_with_reset_added",
			style: Style{
				Foreground: ANSIRed,
				Bold:       StyleSetOn,
				Underline:  StyleSetOff,
			},
			input: "hi",
			want:  "\x1b[1m\x1b[31mhi\x1b[0m",
		},
		{
			name:  "no_styles_noop",
			style: Style{},
			input: "plain",
			want:  "plain",
		},
		{
			name:  "nocolor doesnt do anything",
			style: Style{Foreground: NoColor{}},
			input: "plain",
			want:  "plain",
		},
		{
			name: "keeps_existing_reset",
			style: Style{
				Bold: StyleSetOn,
			},
			input: "hi\x1b[0m",
			want:  "\x1b[1mhi\x1b[0m",
		},
		{
			name:  "empty_string",
			style: Style{Bold: StyleSetOn},
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.style.Wrap(tt.input))
		})
	}
}

func TestApply(t *testing.T) {
	const (
		red       = "\x1b[31m"
		green     = "\x1b[32m"
		bold      = "\x1b[1m"
		italic    = "\x1b[3m"
		reset     = "\x1b[0m"
		normal    = "\x1b[22m"
		defaultFG = "\x1b[39m"
	)

	tests := []struct {
		name  string
		style Style
		input string
		want  string
	}{
		{
			name:  "empty_string_any_style",
			style: Style{Foreground: ANSIRed},
			input: "",
			want:  "",
		},
		{
			name:  "fg_applied_plain_text",
			style: Style{Foreground: ANSIRed},
			input: "hi",
			want:  red + "hi" + reset,
		},
		{
			name:  "no_style_remove_reset",
			style: Style{},
			input: "hello" + reset + " world",
			want:  "hello world",
		},
		{
			name:  "fg_applied_across_reset",
			style: Style{Foreground: ANSIRed},
			input: "hello" + reset + " world",
			want:  red + "hello world" + reset,
		},
		{
			name:  "fg_overrides_existing_color",
			style: Style{Foreground: ANSIRed},
			input: green + "hello" + reset + " world",
			want:  red + "hello world" + reset,
		},
		{
			name:  "fg_overrides_default_fg_reset",
			style: Style{Foreground: ANSIRed},
			input: green + "hello" + defaultFG + " world",
			want:  red + "hello world" + reset,
		},
		{
			name:  "fg_with_embedded_bold_reset",
			style: Style{Foreground: ANSIRed},
			input: bold + "hello" + reset + " world",
			want:  bold + red + "hello" + reset + red + " world" + reset,
		},
		{
			name:  "unset_foreground_removes_color",
			style: Style{Foreground: NoColor{}},
			input: green + "hello" + reset + " world",
			want:  "hello world",
		},
		{
			name:  "unset_bold_add_italic",
			style: Style{Bold: StyleSetOff, Italic: StyleSetOn},
			input: bold + "hello" + reset + " world",
			want:  italic + "hello world" + reset,
		},
		{
			name:  "fg_overrides_bold_normal",
			style: Style{Foreground: ANSIRed},
			input: bold + "hello" + normal + " world",
			want:  bold + red + "hello" + reset + red + " world" + reset,
		},
		{
			name:  "unset_bold_add_italic_with_normal",
			style: Style{Bold: StyleSetOff, Italic: StyleSetOn},
			input: bold + "hello" + normal + " world",
			want:  italic + "hello world" + reset,
		},
		{
			name:  "normal_removed_without_style",
			style: Style{},
			input: "hello" + normal + " world",
			want:  "hello world",
		},
		{
			name:  "trailing_bold_is_ignored",
			style: Style{},
			input: "hello world" + bold,
			want:  "hello world",
		},
		{
			name:  "trailing_reset_is_removed",
			style: Style{},
			input: "hello world" + reset,
			want:  "hello world",
		},
		{
			name:  "unset color when no colors is no op",
			style: Style{Foreground: NoColor{}},
			input: "hello world",
			want:  "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.style.Apply(tt.input))
		})
	}
}
