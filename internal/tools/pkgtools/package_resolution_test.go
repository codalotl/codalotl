package pkgtools

import (
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/stretchr/testify/assert"
)

func TestResolveToolPackageRef_RejectsAbsolutePath(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		abs := pkg.AbsolutePath()
		_, err := resolveToolPackageRef(pkg.Module, abs)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "absolute paths are not allowed")
	})
}

func TestResolveToolPackageRef_RejectsParentTraversal(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		_, err := resolveToolPackageRef(pkg.Module, "../something")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot escape")
	})
}

func TestResolveToolPackageRef_RejectsBackslashes(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		_, err := resolveToolPackageRef(pkg.Module, `internal\tools`)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "backslashes are not allowed")
	})
}

func TestResolveToolPackageRef_NormalizesRelativeDirs(t *testing.T) {
	withSimplePackage(t, func(pkg *gocode.Package) {
		// This uses a deliberately messy-but-relative path to ensure we normalize and resolve.
		rel, err := filepath.Rel(pkg.Module.AbsolutePath, pkg.AbsolutePath())
		if !assert.NoError(t, err) {
			return
		}
		dirty := filepath.ToSlash(filepath.Join(rel, "..", rel))

		res, err := resolveToolPackageRef(pkg.Module, dirty)
		assert.NoError(t, err)
		assert.Equal(t, "mymodule/mypkg", res.ImportPath)
		assert.True(t, isWithinDir(pkg.Module.AbsolutePath, res.PackageAbsDir))
	})
}
