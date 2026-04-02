package pkgtools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveImportPath_ModuleRoot(t *testing.T) {
	fqImportPath, relativeDir, err := resolveImportPath("github.com/codalotl/codalotl", "github.com/codalotl/codalotl")
	assert.NoError(t, err)
	assert.Equal(t, "github.com/codalotl/codalotl", fqImportPath)
	assert.Equal(t, "", relativeDir)
}

func TestResolveImportPath_ModuleRelativePath(t *testing.T) {
	fqImportPath, relativeDir, err := resolveImportPath("github.com/codalotl/codalotl", "internal/tools/pkgtools")
	assert.NoError(t, err)
	assert.Equal(t, "github.com/codalotl/codalotl/internal/tools/pkgtools", fqImportPath)
	assert.Equal(t, "internal/tools/pkgtools", relativeDir)
}

func TestResolveImportPath_RejectsInvalidSegments(t *testing.T) {
	_, _, err := resolveImportPath("github.com/codalotl/codalotl", "../pkgtools")
	assert.Error(t, err)
}
