package docubot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIncorporateFeedbackSuccess(t *testing.T) {
	// Mock LLM response containing an updated documentation snippet for Foo.
	snippet := dedentWithBackticks(`
        // Foo prints hello.
        func Foo() {}
    `)

	conv := &responsesCompleter{responses: []string{snippet}}

	// Original code with an incorrect/outdated doc comment.
	code := dedent(`
        // Foo prints hi.
        func Foo() {}
    `)

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		inc, err := incorporateFeedback(pkg, []IdentifierFeedback{{Identifier: "Foo", Feedback: "Comment should say hello not hi"}}, false, FindFixDocErrorsOptions{
			BaseOptions: BaseOptions{Completer: conv},
		})
		assert.NoError(t, err)

		// Assert useful details about the first IncorporatedFeedback result.
		if assert.NotEmpty(t, inc) {
			c := inc[0]
			assert.Contains(t, c.Change.IDs(), "Foo")
			assert.Contains(t, c.Change.OldCode, "prints hi")
			assert.Contains(t, c.Change.NewCode, "prints hello")
			if assert.NotEmpty(t, c.Feedbacks) {
				var hasFoo bool
				for _, fb := range c.Feedbacks {
					if fb.Identifier == "Foo" {
						hasFoo = true
						break
					}
				}
				assert.True(t, hasFoo, "expected feedback for Foo to be associated with the change")
			}
		}

		// Ensure at least one conversation was started and it referenced Foo.
		if assert.Len(t, conv.convs, 1) {
			joined := strings.Join(conv.convs[0].userMessagesText, "\n")
			assert.Contains(t, joined, "Foo")
		}

		// Reload the package from disk to verify that the documentation was updated.
		newPkg, err := pkg.Reload()
		assert.NoError(t, err)

		snip := newPkg.GetSnippet("Foo")
		if assert.NotNil(t, snip) {
			docs := snip.Docs()
			if assert.NotEmpty(t, docs) {
				assert.Contains(t, docs[0].Doc, "prints hello")
			}
		}
	})
}

func TestIncorporateFeedback_SendsSpecContextWithoutPublicAPI(t *testing.T) {
	snippet := dedentWithBackticks(`
		// Foo prints hello.
		func Foo() {}
	`)
	conv := &responsesCompleter{responses: []string{snippet}}

	code := dedent(`
		// Foo prints hi.
		func Foo() {}
	`)

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		specBody := dedent(`
			# mypkg

			IncorporateSpecMarker should be sent.

			## Public API

			PublicAPIMarker should not be sent.
		`)
		err := os.WriteFile(filepath.Join(pkg.AbsolutePath(), "SPEC.md"), []byte(specBody), 0644)
		require.NoError(t, err)

		_, err = incorporateFeedback(pkg, []IdentifierFeedback{{Identifier: "Foo", Feedback: "Comment should say hello not hi"}}, false, FindFixDocErrorsOptions{
			BaseOptions: BaseOptions{Completer: conv},
		})
		require.NoError(t, err)

		userText := conv.allUserText()
		assert.Contains(t, userText, "IncorporateSpecMarker should be sent.")
		assert.NotContains(t, userText, "PublicAPIMarker should not be sent.")
	})
}

func TestIncorporateFeedbackNoSnippets(t *testing.T) {
	conv := &responsesCompleter{responses: []string{"No code to update"}}

	code := dedent(`
        // Foo prints hi.
        func Foo() {}
    `)

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		inc, err := incorporateFeedback(pkg, []IdentifierFeedback{{Identifier: "Foo", Feedback: "Doc should say hello not hi"}}, false, FindFixDocErrorsOptions{
			BaseOptions: BaseOptions{Completer: conv},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no snippets to apply")
		// Assert that the first return value is nil when no snippets are produced.
		assert.Nil(t, inc)
	})
}
