package docubot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindDocErrorsBatch(t *testing.T) {
	// Mock LLM response: only Foo has an error, Bar is fine (empty string).
	mockResponse := `{"Foo":"Missing details","Bar":""}`
	conv := &responsesCompleter{responses: []string{mockResponse}}

	// Minimal code fixture containing the identifiers we will query.
	code := dedent(`
        // Foo does something.
        func Foo() {}

        // Bar does something.
        func Bar() {}
    `)

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		feedback, err := findDocErrorsBatch(pkg, &gocodecontext.Context{}, []string{"Foo", "Bar"}, FindFixDocErrorsOptions{
			BaseOptions: BaseOptions{Completer: conv},
		})
		assert.NoError(t, err)

		// We expect feedback only for Foo because Bar's message is empty.
		expected := []IdentifierFeedback{{Identifier: "Foo", Feedback: "Missing details"}}
		assert.Equal(t, expected, feedback)

		// Ensure the LLM conversation received both identifiers.
		if assert.Len(t, conv.convs, 1) {
			combinedMsgs := strings.Join(conv.convs[0].userMessagesText, "\n")
			assert.Contains(t, combinedMsgs, "Foo")
			assert.Contains(t, combinedMsgs, "Bar")
		}
	})
}

func TestFindDocErrorsBatch_SendsSpecContextWithoutPublicAPI(t *testing.T) {
	mockResponse := `{"Foo":""}`
	conv := &responsesCompleter{responses: []string{mockResponse}}

	code := dedent(`
		// Foo does something.
		func Foo() {}
	`)

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		specBody := dedent(`
			# mypkg

			FindSpecMarker should be sent.

			## Public API

			PublicAPIMarker should not be sent.
		`)
		err := os.WriteFile(filepath.Join(pkg.AbsolutePath(), "SPEC.md"), []byte(specBody), 0644)
		require.NoError(t, err)

		_, err = findDocErrorsBatch(pkg, &gocodecontext.Context{}, []string{"Foo"}, FindFixDocErrorsOptions{
			BaseOptions: BaseOptions{Completer: conv},
		})
		require.NoError(t, err)

		userText := conv.allUserText()
		assert.Contains(t, userText, "FindSpecMarker should be sent.")
		assert.NotContains(t, userText, "PublicAPIMarker should not be sent.")
	})
}

func TestFindDocErrorsBatch_SpecMDReadErrorIsSurfaced(t *testing.T) {
	conv := &responsesCompleter{responses: []string{`{"Foo":""}`}}

	code := dedent(`
		// Foo does something.
		func Foo() {}
	`)

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		err := os.Mkdir(filepath.Join(pkg.AbsolutePath(), "SPEC.md"), 0755)
		require.NoError(t, err)

		_, err = findDocErrorsBatch(pkg, &gocodecontext.Context{}, []string{"Foo"}, FindFixDocErrorsOptions{
			BaseOptions: BaseOptions{Completer: conv},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "SPEC.md")
		assert.Empty(t, conv.convs)
	})
}

func TestFindAllDocErrors(t *testing.T) {
	// Mock LLM response indicating problems for both Foo and Bar.
	mockResponse := `{"Foo":"Bad docs","Bar":"Bad docs"}`
	// Provide multiple identical responses in case FindAllDocErrors invokes the LLM more than once.
	conv := &responsesCompleter{responses: []string{mockResponse, mockResponse, mockResponse}}

	code := dedent(`
        // Foo does something.
        func Foo() {}

        // Bar does something.
        func Bar() {}
    `)

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		feedbacks, err := findDocErrorsForIDs(pkg, []string{"Foo", "Bar"}, false, FindFixDocErrorsOptions{
			BaseOptions: BaseOptions{Completer: conv, MaxParallelism: 1}, // ensure single goroutine to simplify assertions
		})
		assert.NoError(t, err)

		// We expect feedback for both identifiers regardless of order.
		expected := []IdentifierFeedback{
			{Identifier: "Foo", Feedback: "Bad docs"},
			{Identifier: "Bar", Feedback: "Bad docs"},
		}
		assert.ElementsMatch(t, expected, feedbacks)

		// At least one conversation should have been started.
		assert.GreaterOrEqual(t, len(conv.convs), 1)
	})
}
