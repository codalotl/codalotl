package casclarify

import (
	"testing"

	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	qcas "github.com/codalotl/codalotl/internal/q/cas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendAndRetrieve(t *testing.T) {
	gocodetesting.WithCode(t, "func Exported() {}", func(pkg *gocode.Package) {
		db := testDB(t, pkg)
		first := Entry{
			OriginPackage: "mymodule/caller",
			TargetPackage: "mymodule/mypkg",
			Identifier:    "Exported",
			Question:      "What does it do?",
			Answer:        "It is exported.",
		}
		second := Entry{
			OriginPackage: "mymodule/other",
			TargetPackage: "mymodule/mypkg",
			Identifier:    "package",
			Question:      "What is the package for?",
			Answer:        "It is a test fixture.",
		}

		require.NoError(t, Append(db, pkg, first))
		require.NoError(t, Append(db, pkg, second))

		found, metadata, err := Retrieve(db, pkg)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, Metadata{Entries: []Entry{first, second}}, metadata)
	})
}

func TestRetrieveMissing(t *testing.T) {
	gocodetesting.WithCode(t, "func Exported() {}", func(pkg *gocode.Package) {
		found, metadata, err := Retrieve(testDB(t, pkg), pkg)

		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, metadata.Entries)
	})
}

func testDB(t *testing.T, pkg *gocode.Package) *gocas.DB {
	t.Helper()

	return &gocas.DB{
		BaseDir: pkg.Module.AbsolutePath,
		DB: qcas.DB{
			AbsRoot: t.TempDir(),
		},
	}
}
