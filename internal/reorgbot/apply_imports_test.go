package reorgbot

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyReorganization_CopiesAliasedImportsOnMove(t *testing.T) {
	gocodetesting.WithMultiCode(t, map[string]string{
		"src.go": gocodetesting.Dedent(`
            package mypkg

            import (
                s "strings"
                . "fmt"
                _ "net/http/pprof"
            )

            // UseImports uses both the dot import and the aliased import
            func UseImports(name string) string {
                Println("hi")
                return s.TrimSpace(name)
            }
        `),
	}, func(pkg *gocode.Package) {
		// Build mapping with just the one identifier, moved to a new file.
		s := pkg.GetSnippet("UseImports")
		require.NotNil(t, s)
		id := canonicalSnippetID(s)

		newOrg := map[string][]string{
			"moved.go": {id},
		}
		idToSnippet := map[string]gocode.Snippet{id: s}

		// Apply to the on-disk clone of the temporary module package.
		err := applyReorganization(pkg, newOrg, idToSnippet, false /* onlyTests */)
		require.NoError(t, err)

		// Reload the package from disk to observe the new files and formatting.
		reloaded, err := pkg.Module.ReadPackage(pkg.RelativeDir, nil)
		require.NoError(t, err)

		moved, ok := reloaded.Files["moved.go"]
		require.True(t, ok, "moved.go should exist")
		src := string(moved.Contents)

		// Ensure the aliased imports were copied.
		assert.True(t, strings.Contains(src, "s \"strings\""), "expected aliased import s \"strings\"")
		assert.True(t, strings.Contains(src, ". \"fmt\""), "expected dot import . \"fmt\"")

		// Ensure side-effect import is preserved in the reorganized package (it may be placed in any file).
		presentSomewhere := false
		if strings.Contains(src, "_ \"net/http/pprof\"") {
			presentSomewhere = true
		} else {
			for name, f := range reloaded.Files {
				if name == "moved.go" {
					continue
				}
				if strings.Contains(string(f.Contents), "_ \"net/http/pprof\"") {
					presentSomewhere = true
					break
				}
			}
		}
		assert.True(t, presentSomewhere, "expected side-effect import to be preserved in some file")
	})
}
