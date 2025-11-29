package reorgbot

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResortFile_ReordersWithinFileAndPreservesHeader(t *testing.T) {
	withReorgFixture(t, func(pkg *gocode.Package) {
		// Focus on helpers.go file; gather its ids
		idsByFile := pkg.SnippetsByFile(nil)
		var ids []string
		for _, s := range idsByFile["helpers.go"] {
			id := canonicalSnippetID(s)
			// skip package doc if present
			if _, ok := s.(*gocode.PackageDocSnippet); ok {
				continue
			}
			ids = append(ids, id)
		}
		// Prepare reversed order to assert resort happened; mock LLM returns that
		reversed := make([]string, len(ids))
		copy(reversed, ids)
		sort.Slice(reversed, func(i, j int) bool { return i > j })
		// Build JSON array response for askLLMForFileSort
		var b strings.Builder
		b.WriteString("[")
		for i, id := range reversed {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("\"")
			b.WriteString(id)
			b.WriteString("\"")
		}
		b.WriteString("]")

		conv := &responsesConversationalist{responses: []string{b.String()}}

		// Clone the package on disk to allow file rewrite
		cloned, err := pkg.Clone()
		require.NoError(t, err)
		defer cloned.Module.DeleteClone()

		// Execute
		err = ResortFile(cloned, "helpers.go", ReorgOptions{BaseOptions: BaseOptions{Conversationalist: conv}})
		require.NoError(t, err)

		// Reload to reflect disk changes
		cloned, err = cloned.Module.ReadPackage(cloned.RelativeDir, nil)
		require.NoError(t, err)

		got := string(cloned.Files["helpers.go"].Contents)
		// Header (package and imports) should remain
		require.Contains(t, got, "package mypkg")
		require.Contains(t, got, "import \"strings\"")
		// Ensure the functions are in reversed order by simple index check
		idxNormalize := strings.Index(got, "func NormalizeName(")
		idxClamp := strings.Index(got, "func Clamp(")
		require.Greater(t, idxNormalize, 0)
		require.Greater(t, idxClamp, 0)
		// Reversed means Clamp appears before NormalizeName in file
		require.Less(t, idxClamp, idxNormalize)
	})
}
