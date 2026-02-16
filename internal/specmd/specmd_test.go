package specmd

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRead(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "SPEC.md")
	require.NoError(t, os.WriteFile(p, []byte("hello\n"), 0o644))
	s, err := Read(p)
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(s.AbsPath))
	assert.Equal(t, p, s.AbsPath)
	assert.Equal(t, "hello\n", s.Body)
	_, err = Read(filepath.Join(dir, "NOT_SPEC.md"))
	require.Error(t, err)
}
func TestGoCodeBlocks(t *testing.T) {
	s := &Spec{
		AbsPath: filepath.Join(t.TempDir(), "SPEC.md"),
		Body: strings.Join([]string{
			"# Title",
			"",
			"```go",
			"type Foo struct {",
			"    A int",
			"}",
			"```",
			"",
			"```go",
			"type Bar int",
			"```",
			"",
			"```python",
			"x = 1",
			"```",
			"",
			"```golang",
			"type Baz int",
			"```",
			"",
		}, "\n"),
	}
	blocks, err := s.GoCodeBlocks()
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	assert.Contains(t, blocks[0], "type Foo struct")
	assert.Contains(t, blocks[0], "A int")
}
func TestPublicAPIGoCodeBlocks(t *testing.T) {
	s := &Spec{
		AbsPath: filepath.Join(t.TempDir(), "SPEC.md"),
		Body: strings.Join([]string{
			"# Pkg",
			"",
			"```go",
			"type NotIncluded struct {",
			"    A int",
			"}",
			"```",
			"",
			"## Public API",
			"```go",
			"type Included struct {",
			"    A int",
			"}",
			"```",
			"",
			"### Types",
			"```go",
			"type IncludedToo struct {",
			"    A int",
			"}",
			"```",
			"",
			"## Other",
			"```go {api}",
			"type IncludedByFlag struct {",
			"    A int",
			"}",
			"```",
			"",
		}, "\n"),
	}
	blocks, err := s.PublicAPIGoCodeBlocks()
	require.NoError(t, err)
	require.Len(t, blocks, 3)
	all := strings.Join(blocks, "\n---\n")
	assert.Contains(t, all, "type Included struct")
	assert.Contains(t, all, "type IncludedToo struct")
	assert.Contains(t, all, "type IncludedByFlag struct")
	assert.NotContains(t, all, "type NotIncluded struct")
}
func TestValidate(t *testing.T) {
	ok := &Spec{
		AbsPath: filepath.Join(t.TempDir(), "SPEC.md"),
		Body: strings.Join([]string{
			"# Pkg",
			"",
			"```go",
			"type T struct {",
			"    A int",
			"}",
			"```",
			"",
			"```python",
			"x = 1",
			"```",
			"",
		}, "\n"),
	}
	require.NoError(t, ok.Validate())
	badGo := &Spec{
		AbsPath: filepath.Join(t.TempDir(), "SPEC.md"),
		Body: strings.Join([]string{
			"# Pkg",
			"",
			"```go",
			"func Bad(",
			"```",
			"",
		}, "\n"),
	}
	require.Error(t, badGo.Validate())
	badMD := &Spec{
		AbsPath: filepath.Join(t.TempDir(), "SPEC.md"),
		Body: strings.Join([]string{
			"# Pkg",
			"",
			"```go",
			"type T struct{}",
			"",
		}, "\n"),
	}
	require.Error(t, badMD.Validate())
}
func TestFormatGoCodeBlocks(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "SPEC.md")
	orig := strings.Join([]string{
		"# Pkg",
		"",
		"```go",
		"type  Foo  struct{",
		"A int",
		"}",
		"```",
		"",
		"```go",
		"func Bad(",
		"```",
		"",
	}, "\n")
	require.NoError(t, os.WriteFile(p, []byte(orig), 0o644))
	s, err := Read(p)
	require.NoError(t, err)
	modified, err := s.FormatGoCodeBlocks(0)
	require.NoError(t, err)
	require.True(t, modified)
	updatedBytes, err := os.ReadFile(p)
	require.NoError(t, err)
	updated := string(updatedBytes)
	assert.Contains(t, updated, "type Foo struct {")
	assert.Contains(t, updated, "A int")
	assert.Contains(t, updated, "func Bad(") // invalid Go is ignored and left alone
	assert.Equal(t, updated, s.Body)
}
func TestFormatGoCodeBlocks_ReflowWidth(t *testing.T) {
	countDocLinesAbove := func(code string, funcPrefix string) int {
		lines := strings.Split(code, "\n")
		for i := 0; i < len(lines); i++ {
			if !strings.HasPrefix(lines[i], funcPrefix) {
				continue
			}
			n := 0
			for j := i - 1; j >= 0; j-- {
				if strings.TrimSpace(lines[j]) == "" {
					continue
				}
				if strings.HasPrefix(lines[j], "//") {
					n++
					continue
				}
				break
			}
			return n
		}
		return 0
	}
	getSingleGoFence := func(t *testing.T, s *Spec) string {
		t.Helper()
		md, err := parseMarkdown([]byte(s.Body))
		require.NoError(t, err)
		require.Len(t, md.allGoFences, 1)
		return md.allGoFences[0].code
	}
	orig := strings.Join([]string{
		"# Pkg",
		"",
		"```go",
		"// Foo does a bunch of things and this is a deliberately long sentence that should be wrapped when reflow is enabled.",
		"func Foo() {}",
		"```",
		"",
	}, "\n")
	t.Run("reflowWidth=0 does not reflow docs", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "SPEC.md")
		require.NoError(t, os.WriteFile(p, []byte(orig), 0o644))
		s, err := Read(p)
		require.NoError(t, err)
		_, err = s.FormatGoCodeBlocks(0)
		require.NoError(t, err)
		code := getSingleGoFence(t, s)
		assert.Equal(t, 1, countDocLinesAbove(code, "func Foo"))
	})
	t.Run("reflowWidth>0 reflows docs", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "SPEC.md")
		require.NoError(t, os.WriteFile(p, []byte(orig), 0o644))
		s, err := Read(p)
		require.NoError(t, err)
		_, err = s.FormatGoCodeBlocks(40)
		require.NoError(t, err)
		code := getSingleGoFence(t, s)
		assert.Greater(t, countDocLinesAbove(code, "func Foo"), 1)
	})
}
func TestImplementationDiffs(t *testing.T) {
	modDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(modDir, "go.mod"), []byte(strings.Join([]string{
		"module example.com/tmp",
		"",
		"go 1.24.4",
		"",
	}, "\n")), 0o644))
	pkgDir := filepath.Join(modDir, "mypkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	impl := strings.Join([]string{
		"package mypkg",
		"",
		"// Foo does things.",
		"func Foo(a int) {}",
		"",
		"// DocWS\tdoes things.",
		"func DocWS() {}",
		"",
		"var A int",
		"var B int",
		"",
	}, "\n")
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "impl.go"), []byte(impl), 0o644))
	specBody := strings.Join([]string{
		"# mypkg",
		"",
		"## Public API",
		"```go",
		"// Foo does things.",
		"func Foo(a string) {}",
		"",
		"// DocWS does things.",
		"func DocWS() {}",
		"",
		"var (",
		"    A int",
		"    B int",
		")",
		"",
		"func Missing() {}",
		"```",
		"",
	}, "\n")
	specPath := filepath.Join(pkgDir, "SPEC.md")
	require.NoError(t, os.WriteFile(specPath, []byte(specBody), 0o644))
	s, err := Read(specPath)
	require.NoError(t, err)
	diffs, err := s.ImplemenationDiffs()
	require.NoError(t, err)
	require.NotNil(t, diffs)
	require.Len(t, diffs, 4)
	byID := map[string]SpecDiff{}
	for _, d := range diffs {
		if len(d.IDs) == 0 {
			continue
		}
		byID[d.IDs[0]] = d
	}
	require.Equal(t, DiffTypeCodeMismatch, byID["Foo"].DiffType)
	require.Equal(t, DiffTypeDocWhitespace, byID["DocWS"].DiffType)
	require.Equal(t, DiffTypeIDMismatch, byID["A"].DiffType)
	require.Equal(t, DiffTypeImplMissing, byID["Missing"].DiffType)
}
