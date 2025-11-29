package reorgbot

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_orgIsValid_Validation(t *testing.T) {
	withReorgFixture(t, func(pkg *gocode.Package) {
		ids := pkg.Identifiers(true)
		// Build snippet map
		_, idToSnippet := codeContextForPackage(pkg, ids, false)

		// Valid: split into two files deterministically
		validOrg := map[string][]string{
			"a.go": {},
			"b.go": {},
		}
		// deterministically assign ids alternating
		for i, id := range ids {
			if i%2 == 0 {
				validOrg["a.go"] = append(validOrg["a.go"], id)
			} else {
				validOrg["b.go"] = append(validOrg["b.go"], id)
			}
		}
		require.NoError(t, orgIsValid(validOrg, idToSnippet, false))

		// Missing: drop the last id
		if len(ids) > 0 {
			missingOrg := map[string][]string{"a.go": ids[:len(ids)-1]}
			err := orgIsValid(missingOrg, idToSnippet, false)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "missing:")
		}

		// Extra: add a bogus id
		extraOrg := map[string][]string{"a.go": append([]string{}, ids...)}
		extraOrg["a.go"] = append(extraOrg["a.go"], "NotAnID")
		err := orgIsValid(extraOrg, idToSnippet, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "extra:")

		// Duplicate: repeat first id twice
		if len(ids) > 0 {
			dup := append([]string{}, ids...)
			dup = append(dup, ids[0])
			dupOrg := map[string][]string{"a.go": dup}
			err := orgIsValid(dupOrg, idToSnippet, false)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "duplicates:")
		}
	})
}
