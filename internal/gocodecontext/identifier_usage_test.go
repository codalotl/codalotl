package gocodecontext

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdentifierUsage_InvalidIdentifier(t *testing.T) {
	// IdentifierUsage requires an absolute directory.
	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)
	pkg, err := mod.LoadPackageByRelativeDir("internal/gocodecontext")
	require.NoError(t, err)
	absDir := filepath.Clean(pkg.AbsolutePath())

	_, _, err = IdentifierUsage(absDir, "ThisIdentifierDoesNotExist", false)
	require.Error(t, err)
}

func TestIdentifierUsage_BasicAndExcludesSamePackage(t *testing.T) {
	importPath := "github.com/codalotl/codalotl/internal/gocodecontext"
	targetID := "Groups"

	// Locate the package dir to check exclusion behavior.
	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)
	defPkg, err := mod.LoadPackageByImportPath(importPath)
	require.NoError(t, err)
	defDir := filepath.Clean(defPkg.AbsolutePath())

	usages, summary, err := IdentifierUsage(defDir, targetID, false)
	require.NoError(t, err)

	assert.Contains(t, summary, "--- References ---")
	if len(usages) == 0 {
		assert.Contains(t, summary, "No references found.")
		return
	}

	moduleRoot := mod.AbsolutePath

	for _, u := range usages {
		if filepath.Clean(filepath.Dir(u.AbsFilePath)) == defDir {
			t.Fatalf("usage returned from same package dir: %s", u.AbsFilePath)
		}
		if u.ImportPath == importPath {
			t.Fatalf("usage returned from defining package: %s", u.AbsFilePath)
		}
		if u.Line <= 0 || u.Column <= 0 {
			t.Fatalf("invalid position for usage: line=%d col=%d", u.Line, u.Column)
		}
		if u.FullLine == "" {
			t.Fatalf("FullLine empty for usage: %v", u)
		}
		if u.SnippetFullBytes == "" {
			t.Fatalf("SnippetFullBytes empty for usage: %v", u)
		}
		// If we have any usages, a sanity check that the usage line mentions the identifier.
		if !strings.Contains(u.FullLine, targetID) {
			// Not all usage lines are guaranteed to contain the raw identifier (could be aliased), so warn but don't fail.
			t.Logf("warning: usage line does not contain identifier %q: %q", targetID, strings.TrimRight(u.FullLine, "\n"))
		}

		// Ensure each usage is represented in the string summary.
		relPath, err := filepath.Rel(moduleRoot, filepath.Clean(u.AbsFilePath))
		if err != nil || strings.HasPrefix(relPath, "..") {
			relPath = filepath.ToSlash(filepath.Clean(u.AbsFilePath))
		} else if relPath == "" {
			relPath = filepath.ToSlash(filepath.Base(u.AbsFilePath))
		} else {
			relPath = filepath.ToSlash(relPath)
		}
		if !strings.Contains(summary, relPath) {
			t.Fatalf("summary missing usage path %q:\n%s", relPath, summary)
		}
		lineStr := fmt.Sprintf("%d:\t", u.Line)
		if !strings.Contains(summary, lineStr) {
			t.Fatalf("summary missing line indicator %q:\n%s", lineStr, summary)
		}
	}
}

func TestIdentifierUsage_IncludesIntraPackageUsages(t *testing.T) {
	importPath := "github.com/codalotl/codalotl/internal/gocodecontext"
	targetID := "formatUsagePath"

	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)
	defPkg, err := mod.LoadPackageByImportPath(importPath)
	require.NoError(t, err)
	defDir := filepath.Clean(defPkg.AbsolutePath())

	usages, _, err := IdentifierUsage(defDir, targetID, true)
	require.NoError(t, err)
	require.NotEmpty(t, usages)

	foundIntra := false
	for _, u := range usages {
		if filepath.Clean(filepath.Dir(u.AbsFilePath)) == defDir && u.ImportPath == importPath {
			foundIntra = true
			break
		}
	}
	assert.True(t, foundIntra)
}
