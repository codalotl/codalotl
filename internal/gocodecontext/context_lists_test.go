package gocodecontext

import (
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"

	"slices"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewContextsForIdentifiers(t *testing.T) {
	// Go source with a simple dependency: A calls B. X and Y and W are independent.
	src := Dedent(`
        // A calls B so they share context.
        func A() {
            B()
        }

        func B() {}

        var X = 1
        var Y = 2
        var W = 3
    `)

	gocodetesting.WithCode(t, src, func(pkg *gocode.Package) {
		// Build IdentifierGroups from the parsed package.
		groups, err := Groups(pkg.Module, pkg, GroupOptions{})
		require.NoError(t, err)

		// Identifier list includes one nonexistent id "Z" to ensure it is ignored.
		identifiers := []string{"A", "B", "X", "Z", "Y"}

		ctxMap := NewContextsForIdentifiers(groups, identifiers)

		// Collect all identifiers returned and count occurrences.
		idCount := make(map[string]int)
		for _, ids := range ctxMap {
			for _, id := range ids {
				idCount[id]++
			}
		}

		// Each real identifier appears exactly once.
		for _, id := range []string{"A", "B", "X", "Y"} {
			assert.Equal(t, 1, idCount[id])
		}

		// non-existent identifier "Z" is ignored.
		_, zPresent := idCount["Z"]
		assert.False(t, zPresent)

		// W not prssent, since we didn't ask for it:
		_, wPresent := idCount["W"]
		assert.False(t, wPresent)

		// A and B should be covered by the same context slice.
		abTogether := false
		for _, ids := range ctxMap {
			if slices.Contains(ids, "A") && slices.Contains(ids, "B") {
				abTogether = true
				break
			}
		}
		assert.True(t, abTogether, "A and B should share a context slice (same minimal context)")
	})
}
