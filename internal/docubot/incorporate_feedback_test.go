package docubot

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIncorporateFeedbackSuccess(t *testing.T) {
	// Mock LLM response containing an updated documentation snippet for Foo.
	snippet := dedentWithBackticks(`
        // Foo prints hello.
        func Foo() {}
    `)

	conv := &responsesConversationalist{responses: []string{snippet}}

	// Original code with an incorrect/outdated doc comment.
	code := dedent(`
        // Foo prints hi.
        func Foo() {}
    `)

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		inc, err := incorporateFeedback(pkg, []IdentifierFeedback{{Identifier: "Foo", Feedback: "Comment should say hello not hi"}}, false, FindFixDocErrorsOptions{
			BaseOptions: BaseOptions{Conversationalist: conv},
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

func TestIncorporateFeedbackNoSnippets(t *testing.T) {
	conv := &responsesConversationalist{responses: []string{"No code to update"}}

	code := dedent(`
        // Foo prints hi.
        func Foo() {}
    `)

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		inc, err := incorporateFeedback(pkg, []IdentifierFeedback{{Identifier: "Foo", Feedback: "Doc should say hello not hi"}}, false, FindFixDocErrorsOptions{
			BaseOptions: BaseOptions{Conversationalist: conv},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no snippets to apply")
		// Assert that the first return value is nil when no snippets are produced.
		assert.Nil(t, inc)
	})
}
