package docubot

import (
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/gopackagediff"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPolish_UpdatesDocumentation(t *testing.T) {
	// Original code with sub-optimal documentation.
	originalCode := dedent(`
        // foo is a struct representing foo.
        type Foo struct{}
    `)

	// Expected polished snippet returned by the mock LLM.
	polishedSnippet := dedentWithBackticks(`
        // Foo represents foo and is properly documented.
        type Foo struct{}
    `)

	// Mock conversationalist that returns the polished snippet.
	conv := &responsesConversationalist{responses: []string{
		"Here are the documentation snippets:\n\n" + polishedSnippet,
	}}

	gocodetesting.WithCode(t, originalCode, func(pkg *gocode.Package) {
		// Invoke Polish to update the documentation for Foo.
		changes, err := Polish(pkg, []string{"Foo"}, PolishOptions{BaseOptions: BaseOptions{Conversationalist: conv, MaxParallelism: 1}})
		assert.NoError(t, err)
		assert.True(t, changeSetContains(changes, "Foo"))

		// Reload the package to inspect updated contents.
		pkg, err = pkg.Reload()
		assert.NoError(t, err)

		content := string(pkg.Files["code.go"].Contents)
		assert.Contains(t, content, "Foo represents foo and is properly documented.")
		assert.NotContains(t, content, "foo is a struct representing foo.")
	})
}

func TestPolish_RetryOnSnippetCountMismatch(t *testing.T) {
	// Original code with two identifiers each having sub-optimal documentation.
	originalCode := dedent(`
        // foo is a struct representing foo.
        type Foo struct{}

        // bar does something.
        func Bar() {}
    `)

	// Polished snippets for Foo and Bar respectively.
	polishedFoo := dedentWithBackticks(`
        // Foo represents foo and is properly documented.
        type Foo struct{}
    `)
	polishedBar := dedentWithBackticks(`
        // Bar does something useful.
        func Bar() {}
    `)

	// First LLM response only contains one snippet (mismatched count).
	firstResponse := "Here are the documentation snippets:\n\n" + polishedFoo
	// Second LLM response contains both snippets, matching the expected count.
	secondResponse := "Here are the documentation snippets:\n\n" + polishedFoo + "\n\n" + polishedBar

	// Mock conversationalist set up with the two responses.
	conv := &responsesConversationalist{responses: []string{firstResponse, secondResponse}}

	gocodetesting.WithCode(t, originalCode, func(pkg *gocode.Package) {
		// Request polishing for both identifiers. The first attempt will fail the count check, so Polish must retry.
		changes, err := Polish(pkg, []string{"Foo", "Bar"}, PolishOptions{BaseOptions: BaseOptions{Conversationalist: conv, MaxParallelism: 1}})
		assert.NoError(t, err)
		assert.True(t, changeSetContains(changes, "Foo"))
		assert.True(t, changeSetContains(changes, "Bar"))

		// Ensure that two separate LLM conversations were started (initial + retry).
		assert.Len(t, conv.convs, 2)

		// Verify that the package now contains the polished documentation.
		pkg, err = pkg.Reload()
		assert.NoError(t, err)
		content := string(pkg.Files["code.go"].Contents)
		assert.Contains(t, content, "Foo represents foo and is properly documented.")
		assert.Contains(t, content, "Bar does something useful.")
		assert.NotContains(t, content, "foo is a struct representing foo.")
		assert.NotContains(t, content, "bar does something.")
	})
}

func TestPolish_MultiGroupWithVarBlock(t *testing.T) {
	// Original code containing a var block with two identifiers plus 11 additional exported functions.
	originalCode := dedent(`
        // fooVar and barVar hold sample values.
        var (
            FooVar = 1
            BarVar = 2
        )

        // f0 does something.
        func F0() {}

        // f1 does something.
        func F1() {}

        // f2 does something.
        func F2() {}

        // f3 does something.
        func F3() {}

        // f4 does something.
        func F4() {}

        // f5 does something.
        func F5() {}

        // f6 does something.
        func F6() {}

        // f7 does something.
        func F7() {}

        // f8 does something.
        func F8() {}

        // f9 does something.
        func F9() {}

        // f10 does something.
        func F10() {}
    `)

	// Prepare polished snippets.
	polishedVarBlock := dedentWithBackticks(`
        // FooVar and BarVar hold sample values.
        var (
            FooVar = 1
            BarVar = 2
        )
    `)

	// Helper to create polished function snippet for Fi
	makePolishedFunc := func(i int) string {
		return dedentWithBackticks(strings.ReplaceAll(`
            // FNUM does something useful.
            func FNUM() {}
        `, "FNUM", "F"+fmt.Sprint(i)))
	}

	// Build polished snippets for F0-F10.
	var polishedFuncs []string
	for i := 0; i <= 10; i++ {
		polishedFuncs = append(polishedFuncs, makePolishedFunc(i))
	}

	// Group size is 10, so first response will include var block + F0-F8 (10 snippets) and second response F9-F10 (2 snippets).
	firstResponse := "Here are the documentation snippets:\n\n" + polishedVarBlock + "\n\n" + strings.Join(polishedFuncs[:9], "\n\n")
	secondResponse := "Here are the documentation snippets:\n\n" + strings.Join(polishedFuncs[9:], "\n\n")

	conv := &responsesConversationalist{responses: []string{firstResponse, secondResponse}}

	gocodetesting.WithCode(t, originalCode, func(pkg *gocode.Package) {
		// Prepare the list of identifiers to polish (includes both identifiers from the var block).
		identifiers := []string{"FooVar", "BarVar"}
		for i := 0; i <= 10; i++ {
			identifiers = append(identifiers, fmt.Sprintf("F%d", i))
		}

		changes, err := Polish(pkg, identifiers, PolishOptions{BaseOptions: BaseOptions{Conversationalist: conv, MaxParallelism: 1}})
		assert.NoError(t, err)

		// Expect that all identifiers were reported as changed.
		for _, id := range identifiers {
			assert.True(t, changeSetContains(changes, id))
		}

		// Ensure that two separate LLM conversations were started (one per snippet group).
		assert.Len(t, conv.convs, 2)

		// Verify that documentation was updated for a couple of representatives.
		pkg, err = pkg.Reload()
		assert.NoError(t, err)
		content := string(pkg.Files["code.go"].Contents)
		assert.Contains(t, content, "FooVar and BarVar hold sample values.")
		assert.Contains(t, content, "F0 does something useful.")
		assert.Contains(t, content, "F10 does something useful.")
		// Original, sub-optimal comments should be gone.
		assert.NotContains(t, content, "fooVar and barVar hold sample values.")
		assert.NotContains(t, content, "f0 does something.")
	})
}

// changeSetContains reports whether any change in the set mentions the given identifier in either its old or new identifiers.
func changeSetContains(changes []*gopackagediff.Change, id string) bool {
	for _, c := range changes {
		for _, x := range c.NewIdentifiers {
			if x == id {
				return true
			}
		}
		for _, x := range c.OldIdentifiers {
			if x == id {
				return true
			}
		}
	}
	return false
}
