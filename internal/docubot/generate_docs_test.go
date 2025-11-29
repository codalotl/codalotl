package docubot

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateAndApplyDocs_Basic(t *testing.T) {
	originalCode := dedent(`
        func Foo() {}
    `)

	docSnippet := dedentWithBackticks(`
        // Foo does something.
        func Foo()
    `)

	conv := &responsesConversationalist{responses: []string{
		"Here are the documentation snippets:\n\n" + docSnippet,
	}}

	gocodetesting.WithCode(t, originalCode, func(pkg *gocode.Package) {
		// A minimal, empty code context is sufficient for the happy-path test.
		codeCtx := gocodecontext.NewContext(nil)

		updatedPkg, updatedFiles, err := generateAndApplyDocs(pkg, codeCtx, []string{"Foo"}, false, BaseOptions{Conversationalist: conv})
		require.NoError(t, err)
		require.NotNil(t, updatedPkg)
		assert.Contains(t, updatedFiles, "code.go")

		assert.Contains(t, conv.allUserText(), "- Foo") // Verify that the LLM was instructed to document Foo.

		// Reloaded pkg already contains updates
		assert.Contains(t, string(updatedPkg.Files["code.go"].Contents), "// Foo does something.")

		// Reload the package from disk and ensure updates were persisted:
		pkg, err = updatedPkg.Reload()
		assert.NoError(t, err)

		content := string(pkg.Files["code.go"].Contents)
		assert.Contains(t, content, "// Foo does something.")
	})
}

func TestGenerateAndApplyDocs_RedocumentFalse(t *testing.T) {
	originalCode := dedent(`
        func FooUndoc() {}

		// FooDoc
        func FooDoc() {}
    `)

	docSnippets := []string{
		dedentWithBackticks(`
			// FooUndoc updated
        	func FooUndoc() {}
    	`),
		dedentWithBackticks(`
			// FooDoc updated
        	func FooDoc() {}
    	`),
	}

	conv := &responsesConversationalist{responses: []string{
		"Here are the documentation snippets:\n\n" + strings.Join(docSnippets, "\n\n"),
	}}

	gocodetesting.WithCode(t, originalCode, func(pkg *gocode.Package) {
		// A minimal, empty code context is sufficient for the happy-path test.
		codeCtx := gocodecontext.NewContext(nil)

		updatedPkg, updatedFiles, err := generateAndApplyDocs(pkg, codeCtx, []string{"FooUndoc", "FooDoc"}, false, BaseOptions{Conversationalist: conv})
		require.NoError(t, err)
		require.NotNil(t, updatedPkg)
		assert.Contains(t, updatedFiles, "code.go")
		assert.Contains(t, string(updatedPkg.Files["code.go"].Contents), "// FooUndoc updated")
		assert.NotContains(t, string(updatedPkg.Files["code.go"].Contents), "// FooDoc updated")
	})
}

func TestGenerateAndApplyDocs_RedocumentTrue(t *testing.T) {
	originalCode := dedent(`
        func FooUndoc() {}

		// FooDoc
        func FooDoc() {}
    `)

	docSnippets := []string{
		dedentWithBackticks(`
			// FooUndoc updated
        	func FooUndoc() {}
    	`),
		dedentWithBackticks(`
			// FooDoc updated
        	func FooDoc() {}
    	`),
	}

	conv := &responsesConversationalist{responses: []string{
		"Here are the documentation snippets:\n\n" + strings.Join(docSnippets, "\n\n"),
	}}

	gocodetesting.WithCode(t, originalCode, func(pkg *gocode.Package) {
		// A minimal, empty code context is sufficient for the happy-path test.
		codeCtx := gocodecontext.NewContext(nil)

		updatedPkg, updatedFiles, err := generateAndApplyDocs(pkg, codeCtx, []string{"FooUndoc", "FooDoc"}, true, BaseOptions{Conversationalist: conv})
		require.NoError(t, err)
		require.NotNil(t, updatedPkg)
		assert.Contains(t, updatedFiles, "code.go")
		assert.Contains(t, string(updatedPkg.Files["code.go"].Contents), "// FooUndoc updated")
		assert.Contains(t, string(updatedPkg.Files["code.go"].Contents), "// FooDoc updated")
	})
}

func TestGenerateAndApplyDocs_Fix(t *testing.T) {
	fileToCode := map[string]string{
		"file1.go": dedent(`
            type Foo1 int

            // Bar1 is already documented.
            type Bar1 int
        `),
		"file2.go": dedent(`
            type Foo2 int
            type Bar2 int
        `),
	}

	badFoo1Snippet := dedentWithBackticks(`
        // Foo1 is a foo type.
        type Foo1 int // Foo1 is a foo type.
    `)

	goodFoo1Snippet := dedentWithBackticks(`
        // Foo1 is a foo type.
        type Foo1 int
    `)

	goodBar1Snippet := dedentWithBackticks(`
        // Bar1 updated docs.
        type Bar1 int
    `)

	goodFoo2Snippet := dedentWithBackticks(`
        // Foo2 docs.
        type Foo2 int
    `)

	goodBar2Snippet := dedentWithBackticks(`
        // Bar2 docs.
        type Bar2 int
    `)

	firstResponse := "Here are the documentation snippets:\n\n" + strings.Join([]string{
		badFoo1Snippet,
		goodBar1Snippet,
		goodFoo2Snippet,
		goodBar2Snippet,
	}, "\n\n")

	secondResponse := "Here are the documentation snippets:\n\n" + goodFoo1Snippet

	conv := &responsesConversationalist{responses: []string{firstResponse, secondResponse}}

	gocodetesting.WithMultiCode(t, fileToCode, func(pkg *gocode.Package) {
		codeCtx := gocodecontext.NewContext(nil)

		identifiers := []string{"Foo1", "Bar1", "Foo2", "Bar2"}
		updatedPkg, updatedFiles, err := generateAndApplyDocs(pkg, codeCtx, identifiers, false, BaseOptions{Conversationalist: conv})
		require.NoError(t, err)
		require.NotNil(t, updatedPkg)

		assert.Len(t, updatedFiles, 2)
		assert.Contains(t, updatedFiles, "file1.go")
		assert.Contains(t, updatedFiles, "file2.go")

		file1Content := string(updatedPkg.Files["file1.go"].Contents)
		file2Content := string(updatedPkg.Files["file2.go"].Contents)

		assert.Contains(t, file1Content, "// Foo1 is a foo type.")
		assert.NotContains(t, file1Content, "int // Foo1 is a foo type.")

		assert.Contains(t, file1Content, "// Bar1 is already documented.")
		assert.NotContains(t, file1Content, "// Bar1 updated docs.")

		assert.Contains(t, file2Content, "// Foo2 docs.")
		assert.Contains(t, file2Content, "// Bar2 docs.")
	})
}

func TestGenerateAndApplyDocs_FixDoesntFix(t *testing.T) {
	originalCode := dedent(`
        type Foo1 int
    `)

	badSnippet := dedentWithBackticks(`
        // Foo1 is a foo type.
        type Foo1 int // Foo1 is a foo type.
    `)

	conv := &responsesConversationalist{responses: []string{
		"Here are the documentation snippets:\n\n" + badSnippet,
		"Here are the documentation snippets:\n\n" + badSnippet,
	}}

	gocodetesting.WithCode(t, originalCode, func(pkg *gocode.Package) {
		codeCtx := gocodecontext.NewContext(nil)

		identifiers := []string{"Foo1"}
		updatedPkg, updatedFiles, err := generateAndApplyDocs(pkg, codeCtx, identifiers, false, BaseOptions{Conversationalist: conv})

		assert.ErrorIs(t, err, errSomeSnippetsFailed)
		assert.NotNil(t, updatedPkg)
		assert.Empty(t, updatedFiles)
	})
}

func TestGenerateAndApplyDocs_FixDoesntFixAfterPartialSuccess(t *testing.T) {
	originalCode := dedent(`
        type Foo1 int
        type Foo2 int
        type Foo3 int
    `)

	goodFoo1Snippet := dedentWithBackticks(`
        // Foo1 docs.
        type Foo1 int
    `)

	badFoo2Snippet := dedentWithBackticks(`
        // Foo2 docs.
        type Foo2 int // Foo2 docs.
    `)

	badFoo3Snippet := dedentWithBackticks(`
        // Foo3 docs.
        type Foo3 int // Foo3 docs.
    `)

	goodFoo2Snippet := dedentWithBackticks(`
        // Foo2 docs.
        type Foo2 int
    `)

	firstResponse := "Here are the documentation snippets:\n\n" + strings.Join([]string{
		goodFoo1Snippet,
		badFoo2Snippet,
		badFoo3Snippet,
	}, "\n\n")

	secondResponse := "Here are the documentation snippets:\n\n" + strings.Join([]string{
		goodFoo2Snippet,
		badFoo3Snippet,
	}, "\n\n")

	conv := &responsesConversationalist{responses: []string{firstResponse, secondResponse}}

	gocodetesting.WithCode(t, originalCode, func(pkg *gocode.Package) {
		codeCtx := gocodecontext.NewContext(nil)

		identifiers := []string{"Foo1", "Foo2", "Foo3"}
		updatedPkg, updatedFiles, err := generateAndApplyDocs(pkg, codeCtx, identifiers, false, BaseOptions{Conversationalist: conv})

		assert.ErrorIs(t, err, errSomeSnippetsFailed)
		require.NotNil(t, updatedPkg)

		assert.Len(t, updatedFiles, 1)
		assert.Contains(t, updatedFiles, "code.go")

		content := string(updatedPkg.Files["code.go"].Contents)
		assert.Contains(t, content, "// Foo1 docs.")
		assert.Contains(t, content, "// Foo2 docs.")
		assert.NotContains(t, content, "// Foo3 docs.")
	})
}

func TestGenerateAndApplyDocs_SendsFieldInstructions(t *testing.T) {
	originalCode := dedent(`
        type Person struct {
            // Name is the person's name.
            Name string
            Age  int
            // Address is the person's address.
            Address string
        }
    `)

	docSnippet := dedentWithBackticks(`
        // Person represents a person.
        type Person struct {
            // Name is the person's name.
            Name string
            // Age is the person's age.
            Age int
            // Address is the person's address.
            Address string
        }
    `)

	conv := &responsesConversationalist{responses: []string{
		"Here are the documentation snippets:\n\n" + docSnippet,
	}}

	gocodetesting.WithCode(t, originalCode, func(pkg *gocode.Package) {
		codeCtx := gocodecontext.NewContext(nil)

		updatedPkg, updatedFiles, err := generateAndApplyDocs(pkg, codeCtx, []string{"Person"}, false, BaseOptions{Conversationalist: conv})
		require.NoError(t, err)
		require.NotNil(t, updatedPkg)
		assert.Contains(t, updatedFiles, "code.go")

		userText := conv.allUserText()
		assert.Contains(t, userText, "- Person (including fields: Age -")

		content := string(updatedPkg.Files["code.go"].Contents)
		assert.Contains(t, content, "// Age is the person's age.")
	})
}

func TestMissingFieldDocs_MultiIdentifierSnippet(t *testing.T) {
	originalCode := dedent(`
        type (
            // Foo is documented and its fields are documented.
            Foo struct {
                // A is documented.
                A int
            }

            // Bar is documented but its field is not.
            Bar struct {
                D int
            }
        )
    `)

	gocodetesting.WithCode(t, originalCode, func(pkg *gocode.Package) {
		got := missingFieldDocs(pkg, []string{"Foo", "Bar"})

		// Expect only Bar to appear with missing field D; Foo should not be present.
		require.Contains(t, got, "Bar")
		assert.ElementsMatch(t, []string{"D"}, got["Bar"]) // exact fields for Bar
		assert.NotContains(t, got, "Foo")                  // no fields should be attributed to Foo
	})
}
