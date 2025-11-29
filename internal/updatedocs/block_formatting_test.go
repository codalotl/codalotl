package updatedocs

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDetatchedCommentsInFile_ExcludesFunctionBodyComments(t *testing.T) {
	src := dedent(`
		package p

		// TL floating comment

		type T struct {
			A int // top-level struct field EOL
		}

		func f() {
			// in-func floating comment 1

			type L struct {
				X int // in-func EOL field comment
				// in-func Doc for Y
				Y int
			}
			_ = L{}
			// in-func floating comment 2
		}
	`)

	file := &gocode.File{
		FileName:    "test.go",
		Contents:    []byte(src),
		PackageName: "p",
	}

	fset := token.NewFileSet()
	_, err := file.Parse(fset)
	require.NoError(t, err)

	// Exercise: collect detached comments
	detached := getDetatchedCommentsInFile(file)

	// Helper: flatten comment text for easy matching
	var flattened []string
	for cg := range detached {
		var b strings.Builder
		for _, c := range cg.List {
			b.WriteString(strings.TrimSpace(c.Text))
			b.WriteByte('\n')
		}
		flattened = append(flattened, b.String())
	}

	joined := strings.Join(flattened, "\n---\n")

	// Expectation: only the top-level floating comment should be detected as detached.
	assert.Contains(t, joined, "TL floating comment")
	assert.NotContains(t, joined, "in-func floating comment 1")
	assert.NotContains(t, joined, "in-func floating comment 2")
	assert.NotContains(t, joined, "in-func EOL field comment")
	assert.NotContains(t, joined, "in-func Doc for Y")
}
