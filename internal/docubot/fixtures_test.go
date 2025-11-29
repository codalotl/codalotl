package docubot

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/gopackagediff"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// withCodeFixture loads the testdata files into a temporary in-memory module using gocodetesting.WithMultiCode and then invokes the callback with the parsed package.
func withCodeFixture(t *testing.T, f func(*gocode.Package)) {
	testFiles := []string{"temperature.go", "reading.go", "average.go", "average_test.go", "reading_test.go"}

	fileToCode := make(map[string]string, len(testFiles))
	for _, filename := range testFiles {
		content, err := os.ReadFile(filepath.Join("testdata", filename))
		if !assert.NoError(t, err) {
			return
		}
		fileToCode[filename] = string(content)
	}

	gocodetesting.WithMultiCode(t, fileToCode, f)
}

// filenamesFromChanges collects unique filenames from a set of changes.
func filenamesFromChanges(changes []*gopackagediff.Change) []string {
	set := make(map[string]struct{})
	for _, ch := range changes {
		if ch == nil {
			continue
		}
		if ch.NewSnippet != nil {
			set[ch.NewSnippet.Position().Filename] = struct{}{}
		} else if ch.OldSnippet != nil {
			set[ch.OldSnippet.Position().Filename] = struct{}{}
		}
	}
	var out []string
	for f := range set {
		out = append(out, f)
	}
	return out
}
