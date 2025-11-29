package docubot

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindDocErrorsBatch(t *testing.T) {
	// Mock LLM response: only Foo has an error, Bar is fine (empty string).
	mockResponse := `{"Foo":"Missing details","Bar":""}`
	conv := &responsesConversationalist{responses: []string{mockResponse}}

	// Minimal code fixture containing the identifiers we will query.
	code := dedent(`
        // Foo does something.
        func Foo() {}

        // Bar does something.
        func Bar() {}
    `)

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		feedback, err := findDocErrorsBatch(pkg, &gocodecontext.Context{}, []string{"Foo", "Bar"}, FindFixDocErrorsOptions{
			BaseOptions: BaseOptions{Conversationalist: conv},
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

func TestFindAllDocErrors(t *testing.T) {
	// Mock LLM response indicating problems for both Foo and Bar.
	mockResponse := `{"Foo":"Bad docs","Bar":"Bad docs"}`
	// Provide multiple identical responses in case FindAllDocErrors invokes the LLM more than once.
	conv := &responsesConversationalist{responses: []string{mockResponse, mockResponse, mockResponse}}

	code := dedent(`
        // Foo does something.
        func Foo() {}

        // Bar does something.
        func Bar() {}
    `)

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		feedbacks, err := findDocErrorsForIDs(pkg, []string{"Foo", "Bar"}, false, FindFixDocErrorsOptions{
			BaseOptions: BaseOptions{Conversationalist: conv, MaxParallelism: 1}, // ensure single goroutine to simplify assertions
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
