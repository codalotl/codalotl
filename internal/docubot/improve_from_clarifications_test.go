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

func TestImproveFromClarificationsAppliesUsefulClarification(t *testing.T) {
	code := dedent(`
		// ParseWidget parses widget text.
		func ParseWidget(input string) error { return nil }
	`)
	improvedSnippet := dedentWithBackticks(`
		// ParseWidget parses widget text and returns ErrEmpty when input is empty.
		func ParseWidget(input string) error
	`)
	conv := &responsesCompleter{responses: []string{improvedSnippet}}

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		changes, err := ImproveFromClarifications(pkg, []Clarification{{
			Identifier: "ParseWidget",
			Question:   "What happens when input is empty?",
			Answer:     "ParseWidget returns ErrEmpty when input is empty.",
		}}, ImproveFromClarificationsOptions{
			BaseOptions: BaseOptions{Completer: conv},
		})
		require.NoError(t, err)

		if assert.Len(t, changes, 1) {
			change := changes[0]
			assert.Contains(t, change.Change.IDs(), "ParseWidget")
			assert.Contains(t, change.Change.OldCode, "parses widget text")
			assert.Contains(t, change.Change.NewCode, "returns ErrEmpty")
			if assert.NotEmpty(t, change.Feedbacks) {
				assert.Equal(t, "ParseWidget", change.Feedbacks[0].Identifier)
				assert.Contains(t, change.Feedbacks[0].Feedback, "What happens when input is empty?")
				assert.Contains(t, change.Feedbacks[0].Feedback, "returns ErrEmpty")
			}
		}

		require.Len(t, conv.convs, 1)
		userText := strings.Join(conv.convs[0].userMessagesText, "\n")
		assert.Contains(t, userText, "Question: What happens when input is empty?")
		assert.Contains(t, userText, "Answer: ParseWidget returns ErrEmpty when input is empty.")

		newPkg, err := pkg.Reload()
		require.NoError(t, err)
		snippet := newPkg.GetSnippet("ParseWidget")
		require.NotNil(t, snippet)
		require.NotEmpty(t, snippet.Docs())
		assert.Contains(t, snippet.Docs()[0].Doc, "returns ErrEmpty")
	})
}

func TestImproveFromClarificationsNoOpsWhenLLMReturnsNoSnippets(t *testing.T) {
	code := dedent(`
		// ParseWidget parses widget text.
		func ParseWidget(input string) error { return nil }
	`)
	conv := &responsesCompleter{responses: []string{"No documentation changes."}}

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		changes, err := ImproveFromClarifications(pkg, []Clarification{{
			Identifier: "ParseWidget",
			Question:   "Can ParseWidget parse my local temp file?",
			Answer:     "It can parse the caller's local temp file during this migration.",
		}}, ImproveFromClarificationsOptions{
			BaseOptions: BaseOptions{Completer: conv},
		})
		require.NoError(t, err)
		assert.Empty(t, changes)

		newPkg, err := pkg.Reload()
		require.NoError(t, err)
		content := string(newPkg.Files["code.go"].Contents)
		assert.Contains(t, content, "// ParseWidget parses widget text.")
		assert.NotContains(t, content, "local temp file")
	})
}

func TestImproveFromClarificationsNoOpsForNonPublicOrUnmappedClarifications(t *testing.T) {
	code := dedent(`
		// Foo does foo.
		func Foo() {}

		// helper does helper.
		func helper() {}
	`)
	conv := &responsesCompleter{}

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		changes, err := ImproveFromClarifications(pkg, []Clarification{
			{Identifier: "helper", Question: "Should this be public docs?", Answer: "No."},
			{Identifier: "Missing", Question: "What is Missing?", Answer: "Missing is absent."},
			{Identifier: "Foo", Question: "Anything useful?", Answer: ""},
		}, ImproveFromClarificationsOptions{
			BaseOptions: BaseOptions{Completer: conv},
		})
		require.NoError(t, err)
		assert.Empty(t, changes)
		assert.Empty(t, conv.convs)
	})
}

func TestImproveFromClarificationsSendsSpecContextWithoutPublicAPI(t *testing.T) {
	code := dedent(`
		// Foo does foo.
		func Foo() {}
	`)
	conv := &responsesCompleter{responses: []string{"No documentation changes."}}

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		specBody := dedent(`
			# mypkg

			ClarificationSpecMarker should be sent.

			## Public API

			PublicAPIMarker should not be sent.
		`)
		err := os.WriteFile(filepath.Join(pkg.AbsolutePath(), "SPEC.md"), []byte(specBody), 0644)
		require.NoError(t, err)

		_, err = ImproveFromClarifications(pkg, []Clarification{{
			Identifier: "Foo",
			Question:   "What does Foo do?",
			Answer:     "Foo follows the package specification.",
		}}, ImproveFromClarificationsOptions{
			BaseOptions: BaseOptions{Completer: conv},
		})
		require.NoError(t, err)

		userText := conv.allUserText()
		assert.Contains(t, userText, "ClarificationSpecMarker should be sent.")
		assert.NotContains(t, userText, "PublicAPIMarker should not be sent.")
	})
}
