package diff

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderPretty_DocChange(t *testing.T) {

	a := "// IsTestFunc reports whether f is in a test file and is a TestXxx function, a benchmark, an example, or a fuzz, with the correct signature for each."
	b := "// IsTestFunc reports whether f is in a test file and names a TestXxx, BenchmarkXxx, Example..., or FuzzXxx function. It validates the required signatures for Test, Benchmark, and Fuzz\n// functions, but does not validate Example signatures (any name starting with \"Example\" qualifies)."

	diff := DiffText(a, b)

	// Methodology: if the Println looks good, grab actual from the assert.Equal failure and paste into exp.
	rendered := diff.RenderPretty("a.go", "a.go", 3)
	// fmt.Println(rendered)
	exp := "\x1b[1;36ma.go:\x1b[0m\n\x1b[30m\x1b[48;5;224m-// IsTestFunc reports whether f is in a test file and \x1b[0m\x1b[30m\x1b[48;5;217mi\x1b[0m\x1b[30m\x1b[48;5;224ms a TestXxx\x1b[0m\x1b[30m\x1b[48;5;217m function, a benchmark, an example, or a fuzz, with the correct\x1b[0m\x1b[30m\x1b[48;5;224m signature\x1b[0m\x1b[30m\x1b[48;5;217m for each.\x1b[0m\x1b[30m\x1b[48;5;224m\x1b[0m\n\x1b[30m\x1b[48;5;194m+// IsTestFunc reports whether f is in a test file and \x1b[0m\x1b[30m\x1b[48;5;114mname\x1b[0m\x1b[30m\x1b[48;5;194ms a TestXxx\x1b[0m\x1b[30m\x1b[48;5;114m, BenchmarkXxx, Example..., or FuzzXxx function. It validates the required\x1b[0m\x1b[30m\x1b[48;5;194m signature\x1b[0m\x1b[30m\x1b[48;5;114ms for Test, Benchmark, and Fuzz\x1b[0m\x1b[30m\x1b[48;5;194m\x1b[0m\n\x1b[30m\x1b[48;5;194m+\x1b[0m\x1b[30m\x1b[48;5;114m// functions, but does not validate Example signatures (any name starting with \"Example\" qualifies).\x1b[0m\x1b[30m\x1b[48;5;194m\x1b[0m"
	assert.Equal(t, exp, rendered)
}

func TestRenderPretty_AddIf(t *testing.T) {

	a := `
// foo does something
func foo(x int) int {
	return x
}
`

	b := `
// foo does something
func foo(x int) int {
	if x < 1 {
		return x
	}

	return 0
}
`

	diff := DiffText(a, b)

	// Methodology: if the Println looks good, grab actual from the assert.Equal failure and paste into exp.
	rendered := diff.RenderPretty("a.go", "a.go", 3)
	// fmt.Println(rendered)
	exp := "\x1b[1;36ma.go:\x1b[0m\n\x1b[30m \x1b[0m\n\x1b[30m // foo does something\x1b[0m\n\x1b[30m func foo(x int) int {\x1b[0m\n\x1b[30m\x1b[48;5;224m-\t\x1b[0m\x1b[30m\x1b[48;5;217mreturn x\x1b[0m\x1b[30m\x1b[48;5;224m\x1b[0m\n\x1b[30m\x1b[48;5;194m+\t\x1b[0m\x1b[30m\x1b[48;5;114mif x < 1 {\x1b[0m\x1b[30m\x1b[48;5;194m\x1b[0m\n\x1b[30m\x1b[48;5;194m+\x1b[0m\x1b[30m\x1b[48;5;114m\t\treturn x\x1b[0m\x1b[30m\x1b[48;5;194m\x1b[0m\n\x1b[30m\x1b[48;5;194m+\x1b[0m\x1b[30m\x1b[48;5;114m\t}\x1b[0m\x1b[30m\x1b[48;5;194m\x1b[0m\n\x1b[30m\x1b[48;5;194m+\x1b[0m\n\x1b[30m\x1b[48;5;194m+\x1b[0m\x1b[30m\x1b[48;5;114m\treturn 0\x1b[0m\x1b[30m\x1b[48;5;194m\x1b[0m\n }"
	assert.Equal(t, exp, rendered)
}

func TestRenderPretty_Filename(t *testing.T) {
	// Use a simple change to ensure body lines are present so header positioning is testable.
	baseOld := "old\n"
	baseNew := "new\n"
	d := DiffText(baseOld, baseNew)

	cases := []struct {
		name       string
		from       string
		to         string
		wantHeader string // empty means no header expected
	}{
		{name: "no filenames", from: "", to: "", wantHeader: ""},
		{name: "add file", from: "", to: "somefile.go", wantHeader: "add somefile.go:"},
		{name: "delete file", from: "somefile.go", to: "", wantHeader: "delete somefile.go:"},
		{name: "same name", from: "same.go", to: "same.go", wantHeader: "same.go:"},
		{name: "rename", from: "old.go", to: "new.go", wantHeader: "old.go -> new.go:"},
	}

	const cyanBold = "\x1b[1;36m"
	const reset = "\x1b[0m"

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := d.RenderPretty(tc.from, tc.to, 3)
			firstLine := r
			if idx := strings.Index(firstLine, "\n"); idx >= 0 {
				firstLine = firstLine[:idx]
			}
			if tc.wantHeader == "" {
				assert.False(t, strings.HasPrefix(firstLine, cyanBold), "expected no header, got: %q", firstLine)
				return
			}
			exp := cyanBold + tc.wantHeader + reset
			assert.Equal(t, exp, firstLine)
		})
	}
}

func TestRenderPretty_Context(t *testing.T) {
	// Construct a simple 5-line input with a single change in the middle.
	a := "a\nb\nc\nd\ne\n"
	b := "a\nb\nX\nd\ne\n"

	d := DiffText(a, b)

	// With zero context, surrounding unchanged lines should not appear.
	r0 := d.RenderPretty("", "", 0)
	assert.NotContains(t, r0, " b")
	assert.NotContains(t, r0, " d")

	// With context=1, we expect exactly one unchanged line of context on each side.
	r1 := d.RenderPretty("", "", 1)
	assert.Contains(t, r1, " b")
	assert.Contains(t, r1, " d")
}

func TestRenderUnifiedDiff_SimpleReplace_NoColor(t *testing.T) {
	old := "a\nb\nc\n"
	new := "a\nX\nc\n"

	d := DiffText(old, new)

	r := d.RenderUnifiedDiff(false, "old.go", "new.go", 1)

	exp := strings.Join([]string{
		"--- old.go",
		"+++ new.go",
		"@@ -1,3 +1,3 @@",
		" a",
		"-b",
		"+X",
		" c",
	}, "\n")

	assert.Equal(t, exp, r)
}

func TestRenderUnifiedDiff_SimpleReplace_Color(t *testing.T) {
	old := "a\nb\nc\n"
	new := "a\nX\nc\n"

	d := DiffText(old, new)

	const (
		reset    = "\x1b[0m"
		red      = "\x1b[31m"
		green    = "\x1b[32m"
		magenta  = "\x1b[35m"
		cyanBold = "\x1b[1;36m"
	)

	r := d.RenderUnifiedDiff(true, "old.go", "new.go", 1)

	exp := strings.Join([]string{
		cyanBold + "--- old.go" + reset,
		cyanBold + "+++ new.go" + reset,
		magenta + "@@ -1,3 +1,3 @@" + reset,
		" a",
		red + "-b" + reset,
		green + "+X" + reset,
		" c",
	}, "\n")

	assert.Equal(t, exp, r)
}

func TestRenderUnifiedDiff_MergeBridgedChanges(t *testing.T) {
	old := "a\nb\nc\nd\ne\n"
	new := "a\nX\nc\nY\ne\n"

	d := DiffText(old, new)

	r := d.RenderUnifiedDiff(false, "a.go", "a.go", 1)

	exp := strings.Join([]string{
		"--- a.go",
		"+++ a.go",
		"@@ -1,5 +1,5 @@",
		" a",
		"-b",
		"+X",
		" c",
		"-d",
		"+Y",
		" e",
	}, "\n")

	assert.Equal(t, exp, r)
}
