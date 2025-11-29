package termformat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		tabWidth int
		want     string
	}{
		{
			name:     "plain text unchanged",
			input:    "hello, 世界",
			tabWidth: 4,
			want:     "hello, 世界",
		},
		{
			name:     "tab expanded when width positive",
			input:    "a\tb",
			tabWidth: 3,
			want:     "a   b",
		},
		{
			name:     "tab preserved when width nonpositive",
			input:    "a\tb",
			tabWidth: 0,
			want:     "a\tb",
		},
		{
			name:     "control characters escaped",
			input:    "\x1bX\x00Y\x7f",
			tabWidth: 4,
			want:     "\\x1BX\\x00Y\\x7F",
		},
		{
			name:     "newline and carriage return preserved",
			input:    "line1\r\nline2",
			tabWidth: 4,
			want:     "line1\r\nline2",
		},
		{
			name:     "mixed content handled",
			input:    "A\x00B\tC\nD",
			tabWidth: 2,
			want:     "A\\x00B  C\nD",
		},
		{
			name:     "invalid utf8 replaced",
			input:    string([]byte{0xff, 'a', 0xc1}),
			tabWidth: 4,
			want:     "\ufffda\ufffd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Sanitize(tt.input, tt.tabWidth))
		})
	}
}
