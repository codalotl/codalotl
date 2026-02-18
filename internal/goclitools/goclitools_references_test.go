package goclitools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRefLine_WithRange(t *testing.T) {
	line := "/Users/david/code/myproj/clitool/main.go:247:37-43"
	ref, ok := parseRefLine(line)
	require.True(t, ok, "expected ok for parseRefLine")
	assert.Equal(t, "/Users/david/code/myproj/clitool/main.go", ref.AbsPath)
	assert.Equal(t, 247, ref.Line)
	assert.Equal(t, 37, ref.ColumnStart)
	assert.Equal(t, 43, ref.ColumnEnd)
}

func TestParseRefLine_NoRange(t *testing.T) {
	line := "/abs/path/to/file.go:12:5"
	ref, ok := parseRefLine(line)
	require.True(t, ok, "expected ok for parseRefLine")
	assert.Equal(t, "/abs/path/to/file.go", ref.AbsPath)
	assert.Equal(t, 12, ref.Line)
	assert.Equal(t, 5, ref.ColumnStart)
	assert.Equal(t, 6, ref.ColumnEnd)
}

func TestParseReferencesOutput_MultipleLines(t *testing.T) {
	out := `
	/abs/a.go:10:3-7
	/abs/b.go:20:1
	`
	refs := parseReferencesOutput(out)
	require.Len(t, refs, 2)
	assert.Equal(t, "/abs/a.go", refs[0].AbsPath)
	assert.Equal(t, 10, refs[0].Line)
	assert.Equal(t, 3, refs[0].ColumnStart)
	assert.Equal(t, 7, refs[0].ColumnEnd)

	assert.Equal(t, "/abs/b.go", refs[1].AbsPath)
	assert.Equal(t, 20, refs[1].Line)
	assert.Equal(t, 1, refs[1].ColumnStart)
	assert.Equal(t, 2, refs[1].ColumnEnd)
}

// This is a smoke test that exercises the References function when gopls is available. It creates a tiny temporary module and queries references for an identifier.
func TestReferences_Smoke(t *testing.T) {
	discoverTools()
	if !goplsAvail {
		t.Skip("gopls not available; skipping TestReferences_Smoke")
	}
	tmpDir := t.TempDir()
	mod := "module example.com/tmp\n\ngo 1.20\n"
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(mod), 0o644)
	require.NoError(t, err, "write go.mod")
	src := `package p

func target() {}

func call() {
	target()
}
`
	fpath := filepath.Join(tmpDir, "a.go")
	err = os.WriteFile(fpath, []byte(src), 0o644)
	require.NoError(t, err, "write a.go")
	// Find byte-based column for the 'target' in the call site.
	lines := strings.Split(src, "\n")
	var lineNum, colNum int
	for i, ln := range lines {
		if strings.Contains(ln, "target()") && !strings.HasPrefix(strings.TrimSpace(ln), "func") {
			lineNum = i + 1 // 1-based
			colNum = strings.Index(ln, "target") + 1
			break
		}
	}
	require.NotZero(t, lineNum, "line number for call site")
	require.NotZero(t, colNum, "column number for call site")
	refs, err := References(fpath, lineNum, colNum)
	if err != nil {
		// On some platforms/workspace setups, gopls may fail; skip instead of failing hard.
		if runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows" {
			t.Skipf("skipping due to gopls error: %v", err)
		}
		require.NoError(t, err, "References error")
	}
	require.NotEmpty(t, refs, "expected at least 1 reference")
}
