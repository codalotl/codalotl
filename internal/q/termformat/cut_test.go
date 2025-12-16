package termformat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCut(t *testing.T) {
	red := ANSIRed.ANSISequence(false)

	tests := []struct {
		name  string
		s     string
		left  int
		right int
		want  string
	}{
		{
			name:  "plain",
			s:     "hello",
			left:  1,
			right: 1,
			want:  "ell",
		},
		{
			name:  "wideRuneLeftRemovesWholeCluster",
			s:     "界!",
			left:  1,
			right: 0,
			want:  "!",
		},
		{
			name:  "wideRuneLeftExactRemovesWholeCluster",
			s:     "界!",
			left:  2,
			right: 0,
			want:  "!",
		},
		{
			name:  "wideRuneRightRemovesWholeCluster",
			s:     "!界",
			left:  0,
			right: 1,
			want:  "!",
		},
		{
			name:  "wideRuneRightExactRemovesWholeCluster",
			s:     "!界",
			left:  0,
			right: 2,
			want:  "!",
		},
		{
			name:  "sgrStateFromRemovedLeftIsReapplied",
			s:     red + "hello" + ANSIReset,
			left:  2,
			right: 1,
			want:  red + "ll" + ANSIReset,
		},
		{
			name:  "sgrResetInsideKeptRegionIsPreserved",
			s:     red + "he" + ANSIReset + "llo",
			left:  1,
			right: 1,
			want:  red + "e" + ANSIReset + "ll",
		},
		{
			name:  "noDuplicateLeadingCodesWhenLeftIsZero",
			s:     red + "hi" + ANSIReset,
			left:  0,
			right: 1,
			want:  red + "h" + ANSIReset,
		},
		{
			name:  "negativeArgsTreatedAsZero",
			s:     "hello",
			left:  -1,
			right: -2,
			want:  "hello",
		},
		{
			name:  "removesAllWidth",
			s:     "hello",
			left:  10,
			right: 0,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Cut(tt.s, tt.left, tt.right))
		})
	}
}
