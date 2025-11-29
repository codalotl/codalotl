package diff

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiffText_DocChange(t *testing.T) {
	// This smoke test replaces a 1-line comment with a 2-line variant.

	a := "// IsTestFunc reports whether f is in a test file and is a TestXxx function, a benchmark, an example, or a fuzz, with the correct signature for each.\n"
	b := "// IsTestFunc reports whether f is in a test file and names a TestXxx, BenchmarkXxx, Example..., or FuzzXxx function. It validates the required signatures for Test, Benchmark, and Fuzz\n// functions, but does not validate Example signatures (any name starting with \"Example\" qualifies).\n"

	diff := DiffText(a, b)

	if err := diff.validate(); err != nil {
		require.NoError(t, err)
	}

	// 1 hunk, a replace, with 2 lines:
	require.Len(t, diff.Hunks, 1)
	hunk := diff.Hunks[0]
	require.Equal(t, OpReplace, hunk.Op)
	require.Len(t, hunk.Lines, 2)

	// Lines are Replace and Insert:
	line1 := hunk.Lines[0]
	line2 := hunk.Lines[1]
	require.Equal(t, OpReplace, line1.Op)
	require.Equal(t, OpInsert, line2.Op)

	// Check line2 first, since it unambiguously adds a single insert span:
	require.Len(t, line2.Spans, 1)
	span := line2.Spans[0]
	require.Equal(t, OpInsert, span.Op)
	require.Equal(t, "// functions, but does not validate Example signatures (any name starting with \"Example\" qualifies).", span.NewText)

	//
	// Check line1. At this point, there may be some ambiguity in implementation depending on tuning (ex: how much do we smash together small changes). Check unambiguious things first.
	//

	// Unambiguous: at least 2 spans, a prefix and a replace:
	require.GreaterOrEqual(t, len(line1.Spans), 2)
	require.Equal(t, OpEqual, line1.Spans[0].Op)
	require.Equal(t, "// IsTestFunc reports whether f is in a test file and ", line1.Spans[0].OldText)
	require.Equal(t, OpReplace, line1.Spans[1].Op)

	// At this point, just locking in "reasonable" spans. If these start failing, make sure the new spans are reasonable and just lock them in:
	require.Equal(t, "i", line1.Spans[1].OldText)
	require.Equal(t, "name", line1.Spans[1].NewText)
	require.Equal(t, len(line1.Spans), 6)

	require.Equal(t, OpEqual, line1.Spans[2].Op)
	require.Equal(t, "s a TestXxx", line1.Spans[2].OldText)

	require.Equal(t, OpReplace, line1.Spans[3].Op)
	require.Equal(t, " function, a benchmark, an example, or a fuzz, with the correct", line1.Spans[3].OldText)
	require.Equal(t, ", BenchmarkXxx, Example..., or FuzzXxx function. It validates the required", line1.Spans[3].NewText)

	require.Equal(t, OpEqual, line1.Spans[4].Op)
	require.Equal(t, " signature", line1.Spans[4].OldText)

	require.Equal(t, OpReplace, line1.Spans[5].Op)
	require.Equal(t, " for each.", line1.Spans[5].OldText)
	require.Equal(t, "s for Test, Benchmark, and Fuzz", line1.Spans[5].NewText)

	// Look at spans:
	// for _, s := range line1.Spans {
	// 	fmt.Println(s)
	// }
}

func TestDiffText_Hunks(t *testing.T) {
	type hunkExpectation struct {
		op  Op
		old string
		new string
	}

	tests := []struct {
		name string
		old  string
		new  string
		want []hunkExpectation
	}{
		{
			name: "add whole file",
			old:  "",
			new:  "a\nb\n",
			want: []hunkExpectation{{op: OpInsert, old: "", new: "a\nb\n"}},
		},
		{
			name: "delete whole file",
			old:  "a\nb\n",
			new:  "",
			want: []hunkExpectation{{op: OpDelete, old: "a\nb\n", new: ""}},
		},
		{
			name: "no newlines - equal",
			old:  "hello",
			new:  "hello",
			want: []hunkExpectation{{op: OpEqual, old: "hello", new: "hello"}},
		},
		{
			name: "no newlines - add word in beginning",
			old:  "world",
			new:  "hello world",
			want: []hunkExpectation{{op: OpReplace, old: "world", new: "hello world"}},
		},
		{
			name: "no newlines - add word in middle",
			old:  "a c",
			new:  "a b c",
			want: []hunkExpectation{{op: OpReplace, old: "a c", new: "a b c"}},
		},
		{
			name: "no newlines - add word in end",
			old:  "hello",
			new:  "hello world",
			want: []hunkExpectation{{op: OpReplace, old: "hello", new: "hello world"}},
		},
		{
			name: "no newlines - delete word in middle",
			old:  "a b c",
			new:  "a c",
			want: []hunkExpectation{{op: OpReplace, old: "a b c", new: "a c"}},
		},
		{
			name: "no newlines - replace words",
			old:  "hello world",
			new:  "hello there",
			want: []hunkExpectation{{op: OpReplace, old: "hello world", new: "hello there"}},
		},
		{
			name: "equal whole text",
			old:  "a\nb\n",
			new:  "a\nb\n",
			want: []hunkExpectation{{op: OpEqual, old: "a\nb\n", new: "a\nb\n"}},
		},
		{
			name: "insert at end",
			old:  "a\nb\n",
			new:  "a\nb\nc\n",
			want: []hunkExpectation{
				{op: OpEqual, old: "a\nb\n", new: "a\nb\n"},
				{op: OpInsert, old: "", new: "c\n"},
			},
		},
		{
			name: "delete at end",
			old:  "a\nb\nc\n",
			new:  "a\nb\n",
			want: []hunkExpectation{
				{op: OpEqual, old: "a\nb\n", new: "a\nb\n"},
				{op: OpDelete, old: "c\n", new: ""},
			},
		},
		{
			name: "replace middle line",
			old:  "a\nb\nc\n",
			new:  "a\nX\nc\n",
			want: []hunkExpectation{
				{op: OpEqual, old: "a\n", new: "a\n"},
				{op: OpReplace, old: "b\n", new: "X\n"},
				{op: OpEqual, old: "c\n", new: "c\n"},
			},
		},
		{
			name: "no trailing newline replace",
			old:  "a\nb",
			new:  "a\nbc",
			want: []hunkExpectation{
				{op: OpEqual, old: "a\n", new: "a\n"},
				{op: OpReplace, old: "b", new: "bc"},
			},
		},
		{
			name: "windows - rn just kinda works",
			old:  "a\r\nb\r\n",
			new:  "a\r\nX\r\n",
			want: []hunkExpectation{
				{op: OpEqual, old: "a\r\n", new: "a\r\n"},
				{op: OpReplace, old: "b\r\n", new: "X\r\n"},
			},
		},
		{
			name: "multiple edits",
			old:  "a\nb\nc\nd\ne\n",
			new:  "a\nz\nc\ny\ne\n",
			want: []hunkExpectation{
				{op: OpEqual, old: "a\n", new: "a\n"},
				{op: OpReplace, old: "b\n", new: "z\n"},
				{op: OpEqual, old: "c\n", new: "c\n"},
				{op: OpReplace, old: "d\n", new: "y\n"},
				{op: OpEqual, old: "e\n", new: "e\n"},
			},
		},
		{
			name: "insert and delete",
			old:  "a\nb\nc\nd\ne\n",
			new:  "a\nb\nz\nc\ne\n",
			want: []hunkExpectation{
				{op: OpEqual, old: "a\nb\n", new: "a\nb\n"},
				{op: OpInsert, old: "", new: "z\n"},
				{op: OpEqual, old: "c\n", new: "c\n"},
				{op: OpDelete, old: "d\n", new: ""},
				{op: OpEqual, old: "e\n", new: "e\n"},
			},
		},
		{
			name: "multiple inserted lines are coalesced into a single hunk",
			old:  "a\nb\nc\nd\ne\n",
			new:  "a\nb\nz\ny\nx\nd\ne\n",
			want: []hunkExpectation{
				{op: OpEqual, old: "a\nb\n", new: "a\nb\n"},
				{op: OpReplace, old: "c\n", new: "z\ny\nx\n"},
				{op: OpEqual, old: "d\ne\n", new: "d\ne\n"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := DiffText(tc.old, tc.new)

			// Keep d.validate() here even if DiffText also calls it. DiffText calling it is temporary.
			if err := d.validate(); err != nil {
				require.Fail(t, fmt.Sprintf("%s: validate produced err=%v", tc.name, err))
			}

			// Expected top-level hunks
			got := make([]hunkExpectation, 0, len(d.Hunks))
			for _, h := range d.Hunks {
				got = append(got, hunkExpectation{op: h.Op, old: h.OldText, new: h.NewText})
			}
			require.Equal(t, tc.want, got)
		})
	}
}
