package specmd

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFormatDiffs(t *testing.T) {
	var buf bytes.Buffer
	diffs := []SpecDiff{
		{
			IDs:         []string{"Foo"},
			SpecSnippet: "func Foo(a int)\n",
			SpecLine:    12,
			ImplSnippet: "func Foo(a string)\n",
			ImplFile:    "impl.go",
			ImplLine:    3,
			DiffType:    DiffTypeCodeMismatch,
		},
		{
			IDs:         []string{"A", "B"},
			SpecSnippet: "var (\n\tA int\n\tB int\n)\n",
			SpecLine:    20,
			ImplSnippet: "",
			ImplFile:    "",
			ImplLine:    0,
			DiffType:    DiffTypeImplMissing,
		},
	}
	require.NoError(t, FormatDiffs(diffs, &buf))
	out := buf.String()
	assert.Contains(t, out, "DIFF 1/2\n")
	assert.Contains(t, out, "type: code-mismatch\n")
	assert.Contains(t, out, "ids: Foo\n")
	assert.Contains(t, out, "spec: SPEC.md:12\n")
	assert.Contains(t, out, "impl: impl.go:3\n")
	assert.Contains(t, out, "SPEC:\n```go\nfunc Foo(a int)\n```\n")
	assert.Contains(t, out, "IMPL:\n```go\nfunc Foo(a string)\n```\n")

	assert.Contains(t, out, "DIFF 2/2\n")
	assert.Contains(t, out, "type: impl-missing\n")
	assert.Contains(t, out, "ids: [A, B]\n")
	assert.Contains(t, out, "spec: SPEC.md:20\n")
	assert.Contains(t, out, "impl: <missing>\n")
}

func TestFormatDiffs_NilWriter(t *testing.T) {
	require.Error(t, FormatDiffs([]SpecDiff{{IDs: []string{"Foo"}}}, nil))
}

func TestFormatDiffs_EmptyDiffs(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, FormatDiffs(nil, &buf))
	assert.Equal(t, "", buf.String())
}
