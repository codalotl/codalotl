package gocode

import (
	"fmt"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type expectedSnippet struct {
	hasExported      bool
	test             bool
	hasPublicSnippet bool
	publicSnippet    []byte
	fullBytes        []byte
	docs             []IdentifierDocumentation
	missingDocs      []IdentifierDocumentation
}

// expectedSnippet implements Snippet
var _ Snippet = (*expectedSnippet)(nil)

func (e *expectedSnippet) HasExported() bool {
	return e.hasExported
}

// IDs returns nil and exists only to satisfy the Snippet interface in tests.
func (e *expectedSnippet) IDs() []string {
	return nil
}

// Test returns the test field value.
func (e *expectedSnippet) Test() bool {
	return e.test
}

func (e *expectedSnippet) Bytes() []byte {
	panic("unimplemented")
}

func (e *expectedSnippet) FullBytes() []byte {
	return e.fullBytes
}

// PublicSnippet returns the publicSnippet field and any error.
func (e *expectedSnippet) PublicSnippet() ([]byte, error) {
	return e.publicSnippet, nil
}

// Docs returns the docs field.
func (e *expectedSnippet) Docs() []IdentifierDocumentation {
	return e.docs
}

// MissingDocs returns the missingDocs field.
func (e *expectedSnippet) MissingDocs() []IdentifierDocumentation {
	return e.missingDocs
}

func (e *expectedSnippet) Position() token.Position {
	return token.Position{}
}

type expectedFunc struct {
	name         string
	receiverType string
	identifier   string
	sig          string
	hasDoc       bool
	docContains  string
	snippet      *expectedSnippet
}

type expectedValue struct {
	identifiers      []string
	isVar            bool
	isBlock          bool
	hasBlockDoc      bool
	blockDocContains string
	identifierDocs   map[string]string
	snippetBytes     string // if zero value, ignored; if present, snippet must match exactly
	snippet          *expectedSnippet
}

type expectedType struct {
	identifiers      []string
	isBlock          bool
	hasBlockDoc      bool
	blockDocContains string
	identifierDocs   map[string]string
	fieldDocs        map[string]string // field-key -> doc for field documentation testing
	snippetBytes     string            // if zero value, ignored; if present, snippet must match exactly
	snippet          *expectedSnippet
}

type expectedPackageDoc struct {
	hasDoc       bool
	docs         string
	snippetBytes string
	snippet      *expectedSnippet
}

// extractSnippetsFromSource is a test helper that parses source code and extracts all snippet types.
func extractSnippetsFromSource(t *testing.T, source string) ([]*FuncSnippet, []*ValueSnippet, []*TypeSnippet, *PackageDocSnippet, error) {
	t.Helper()

	var fullSource string
	// If source already contains "package", use it as-is, otherwise inject package declaration
	if strings.Contains(source, "package ") {
		fullSource = source
	} else {
		fullSource = "package testpkg\n\n" + source
	}

	// Create and parse File
	goFile := &File{
		FileName:         "test.go",
		RelativeFileName: "test.go",
		AbsolutePath:     "/tmp/test.go",
		Contents:         []byte(fullSource),
		PackageName:      "testpkg",
		IsTest:           false,
	}

	fset := token.NewFileSet()
	_, err := goFile.Parse(fset)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// Extract snippets
	return extractSnippets(goFile)
}

// assertSnippetInterface validates the Snippet interface methods against expected values.
func assertSnippetInterface(t *testing.T, got Snippet, want *expectedSnippet) {
	t.Helper()
	if want == nil {
		return
	}

	assert.Equal(t, want.hasExported, got.HasExported(), "HasExported mismatch")
	assert.Equal(t, want.test, got.Test(), "Test mismatch")

	if want.hasPublicSnippet {
		actualPublicSnippet, err := got.PublicSnippet()
		require.NoError(t, err, "PublicSnippet() should not return an error")
		if !assert.Equal(t, want.publicSnippet, actualPublicSnippet, "PublicSnippet() mismatch") {
			fmt.Println("Want:")
			fmt.Println(string(want.publicSnippet))
			fmt.Println("Got:")
			fmt.Println(string(actualPublicSnippet))
		}
	}
	if want.fullBytes != nil {
		// NOTE: dedent always adds a \n to want.fullBytes, and got.FullBytes() correctly has no \n.
		assert.Equal(t, strings.TrimSpace(string(want.fullBytes)), string(got.FullBytes()), "FullBytes mismatch")
	}

	assert.Equal(t, want.docs, got.Docs(), "Docs() mismatch")
	assert.Equal(t, want.missingDocs, got.MissingDocs(), "MissingDocs() mismatch")
}

// assertFuncSnippet validates a function snippet against expected values.
func assertFuncSnippet(t *testing.T, got *FuncSnippet, want expectedFunc) {
	t.Helper()

	assert.Equal(t, want.name, got.Name, "Function name mismatch")
	assert.Equal(t, want.receiverType, got.ReceiverType, "Receiver type mismatch")
	assert.Equal(t, want.identifier, got.Identifier, "Identifier mismatch")
	assert.Equal(t, want.sig, got.Sig, "Function signature mismatch")
	assert.Equal(t, "test.go", got.FileName, "File name should be test.go")

	// Check documentation
	if want.hasDoc {
		assert.NotEmpty(t, got.Doc, "Function should have documentation")
		if want.docContains != "" {
			assert.Contains(t, got.Doc, want.docContains, "Documentation should contain expected text")
		}
	} else {
		assert.Empty(t, got.Doc, "Function should not have documentation")
	}

	// Verify snippet and signature are populated
	assert.NotEmpty(t, got.Snippet, "Snippet should not be empty")
	assert.NotEmpty(t, got.Sig, "Signature should not be empty")

	// Verify FullFunc:
	assert.NotEmpty(t, got.FullFunc, "FullFunc should not be empty")
	if !strings.Contains(string(got.FullFunc), "{") {
		assert.Equal(t, string(got.Snippet), string(got.FullFunc), "FullFunc should equal Snippet for functions without bodies")
	}

	// Check snippet interface methods if expected snippet is provided
	if want.snippet != nil {
		assertSnippetInterface(t, got, want.snippet)
	}
}

// assertValueSnippet validates a value snippet against expected values.
func assertValueSnippet(t *testing.T, got *ValueSnippet, want expectedValue) {
	t.Helper()

	assert.Equal(t, want.identifiers, got.Identifiers, "Identifiers mismatch")
	assert.Equal(t, want.isVar, got.IsVar, "IsVar mismatch")
	assert.Equal(t, want.isBlock, got.IsBlock, "IsBlock mismatch")
	assert.Equal(t, "test.go", got.FileName, "File name should be test.go")

	// Check block documentation
	if want.hasBlockDoc {
		assert.NotEmpty(t, got.BlockDoc, "Value should have block documentation")
		if want.blockDocContains != "" {
			assert.Contains(t, got.BlockDoc, want.blockDocContains, "Block documentation should contain expected text")
		}
	} else {
		assert.Empty(t, got.BlockDoc, "Value should not have block documentation")
	}

	// Check identifier documentation
	assert.Equal(t, len(want.identifiers), len(got.IdentifierDocs), "Length of identifiers should match length of IdentifierDocs")

	// For every key in want.identifierDocs, assert exact match
	for key, expectedDoc := range want.identifierDocs {
		assert.Equal(t, expectedDoc, got.IdentifierDocs[key], "IdentifierDocs[%s] should exactly match expected content", key)
	}

	// For every identifier, check that if it's not in want.identifierDocs, then got.IdentifierDocs[key] must be empty string
	for _, identifier := range want.identifiers {
		if _, exists := want.identifierDocs[identifier]; !exists {
			assert.Equal(t, "", got.IdentifierDocs[identifier], "IdentifierDocs[%s] should be empty string when not expected", identifier)
		}
	}

	// Verify snippet is populated
	assert.NotEmpty(t, got.Snippet, "Snippet should not be empty")

	// Check snippet exact match if provided
	if want.snippetBytes != "" {
		assert.Equal(t, want.snippetBytes, string(got.Snippet), "Snippet should exactly match expected content")
	}

	// Check snippet interface methods if expected snippet is provided
	if want.snippet != nil {
		assertSnippetInterface(t, got, want.snippet)
	}
}

// assertTypeSnippet validates a type snippet against expected values.
func assertTypeSnippet(t *testing.T, got *TypeSnippet, want expectedType) {
	t.Helper()

	assert.Equal(t, want.identifiers, got.Identifiers, "Identifiers mismatch")
	assert.Equal(t, want.isBlock, got.IsBlock, "IsBlock mismatch")
	assert.Equal(t, "test.go", got.FileName, "File name should be test.go")

	// Check block documentation
	if want.hasBlockDoc {
		assert.NotEmpty(t, got.BlockDoc, "Type should have block documentation")
		if want.blockDocContains != "" {
			assert.Contains(t, got.BlockDoc, want.blockDocContains, "Block documentation should contain expected text")
		}
	} else {
		assert.Empty(t, got.BlockDoc, "Type should not have block documentation")
	}

	// Check identifier documentation
	assert.Equal(t, len(want.identifiers), len(got.IdentifierDocs), "Length of identifiers should match length of IdentifierDocs")

	// For every key in want.identifierDocs, assert exact match
	for key, expectedDoc := range want.identifierDocs {
		assert.Equal(t, expectedDoc, got.IdentifierDocs[key], "IdentifierDocs[%s] should exactly match expected content", key)
	}

	// For every identifier, check that if it's not in want.identifierDocs, then got.IdentifierDocs[key] must be empty string
	for _, identifier := range want.identifiers {
		if _, exists := want.identifierDocs[identifier]; !exists {
			assert.Equal(t, "", got.IdentifierDocs[identifier], "IdentifierDocs[%s] should be empty string when not expected", identifier)
		}
	}

	// Check field documentation
	if want.fieldDocs != nil {
		// For every key in want.fieldDocs, assert exact match
		for fieldKey, expectedDoc := range want.fieldDocs {
			actualDoc, exists := got.FieldDocs[fieldKey]
			assert.True(t, exists, "FieldDocs should contain key %s", fieldKey)
			assert.Equal(t, expectedDoc, actualDoc, "FieldDocs[%s] should exactly match expected content", fieldKey)
		}

		// Check that we don't have unexpected field docs
		for fieldKey := range got.FieldDocs {
			if _, exists := want.fieldDocs[fieldKey]; !exists {
				// If not expected, the field doc should be empty
				assert.Equal(t, "", got.FieldDocs[fieldKey], "FieldDocs[%s] should be empty string when not expected", fieldKey)
			}
		}
	} else {
		// If no field docs expected, FieldDocs should be nil or empty
		if got.FieldDocs != nil {
			for fieldKey, doc := range got.FieldDocs {
				assert.Equal(t, "", doc, "FieldDocs[%s] should be empty when no field docs expected", fieldKey)
			}
		}
	}

	// Verify snippet is populated
	assert.NotEmpty(t, got.Snippet, "Snippet should not be empty")

	// Check snippet exact match if provided
	if want.snippetBytes != "" {
		assert.Equal(t, want.snippetBytes, string(got.Snippet), "Snippet should exactly match expected content")
	}

	// Check snippet interface methods if expected snippet is provided
	if want.snippet != nil {
		assertSnippetInterface(t, got, want.snippet)
	}
}

// assertPackageDocSnippet validates a package doc snippet against expected values.
func assertPackageDocSnippet(t *testing.T, got *PackageDocSnippet, want expectedPackageDoc) {
	t.Helper()

	assert.Equal(t, "test.go", got.FileName, "File name should be test.go")

	// Check documentation
	if want.hasDoc {
		assert.NotEmpty(t, got.Doc, "PackageDoc should have documentation")
		if want.docs != "" {
			assert.Equal(t, want.docs, got.Doc, "Documentation should exactly match expected content")
		}
	} else {
		assert.Empty(t, got.Doc, "PackageDoc should not have documentation")
	}

	// Check snippet contents
	if want.snippetBytes != "" {
		assert.Equal(t, want.snippetBytes, string(got.Snippet), "Snippet should exactly match expected content")
	}

	// Check snippet interface methods if expected snippet is provided
	if want.snippet != nil {
		assertSnippetInterface(t, got, want.snippet)
	}
}

// dedent removes any common leading indentation from every non-blank line, allowing readable tests with multi-line strings that do not break indentation hierarchy.
// Note: gocode cannot use gocodetesting because the latter uses this package.
func dedent(s string) string {
	s = strings.Trim(s, "\n") // drop leading/trailing blank lines
	lines := strings.Split(s, "\n")

	min := -1 // smallest indent seen so far
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" { // If the line is only whitespace, consider it fully blank
			lines[i] = ""
			continue
		}
		indent := len(line) - len(trimmed)
		if min == -1 || indent < min {
			min = indent
		}
	}

	if min > 0 { // nothing to do if min == 0 or no non‑blank lines
		for i, line := range lines {
			if len(line) >= min {
				lines[i] = line[min:]
			}
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), " \t\n") + "\n"
}

// TestExtractSnippets tests extractSnippets with various Go source code patterns.
func TestExtractSnippets(t *testing.T) {
	tests := []struct {
		name           string
		source         string // Go source code without package declaration
		wantFuncs      []expectedFunc
		wantValues     []expectedValue
		wantTypes      []expectedType
		wantPackageDoc *expectedPackageDoc
	}{
		{
			name:   "simple function without documentation",
			source: `func Hello() { fmt.Println("hello") }`,
			wantFuncs: []expectedFunc{
				{
					name:       "Hello",
					identifier: "Hello",
					sig:        "func Hello()",
					snippet: &expectedSnippet{
						hasExported:      true,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    []byte("func Hello()"),
						fullBytes:        []byte(`func Hello() { fmt.Println("hello") }`),
						docs:             nil,
						missingDocs:      []IdentifierDocumentation{{Identifier: "Hello"}},
					},
				},
			},
		},
		{
			name: "function with documentation",
			source: dedent(`
				// Add adds two integers and returns their sum.
				func Add(a, b int) int {
					return a + b
				}
			`),
			wantFuncs: []expectedFunc{
				{
					name:        "Add",
					identifier:  "Add",
					sig:         "func Add(a, b int) int",
					hasDoc:      true,
					docContains: "Add adds two integers",
					snippet: &expectedSnippet{
						hasExported:      true,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    []byte("// Add adds two integers and returns their sum.\nfunc Add(a, b int) int"),
						fullBytes: []byte(dedent(`
							// Add adds two integers and returns their sum.
							func Add(a, b int) int {
								return a + b
							}`)),
						docs: []IdentifierDocumentation{
							{Identifier: "Add", Doc: "// Add adds two integers and returns their sum.\n"},
						},
						missingDocs: nil,
					},
				},
			},
		},
		{
			name: "method with pointer receiver",
			source: dedent(`
				// ProcessData processes the data in the struct.
				func (s *DataProcessor) ProcessData() error {
					return nil
				}
			`),
			wantFuncs: []expectedFunc{
				{
					name:         "ProcessData",
					receiverType: "*DataProcessor",
					identifier:   "*DataProcessor.ProcessData",
					sig:          "func (s *DataProcessor) ProcessData() error",
					hasDoc:       true,
					docContains:  "ProcessData processes the data",
					snippet: &expectedSnippet{
						hasExported:      true,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    []byte("// ProcessData processes the data in the struct.\nfunc (s *DataProcessor) ProcessData() error"),
						fullBytes: []byte(dedent(`
							// ProcessData processes the data in the struct.
							func (s *DataProcessor) ProcessData() error {
								return nil
							}`)),
						docs: []IdentifierDocumentation{
							{Identifier: "*DataProcessor.ProcessData", Doc: "// ProcessData processes the data in the struct.\n"},
						},
						missingDocs: nil,
					},
				},
			},
		},
		{
			name: "method with value receiver",
			source: dedent(`
				func (c Calculator) Multiply(x, y float64) float64 {
					return x * y
				}
			`),
			wantFuncs: []expectedFunc{
				{
					name:         "Multiply",
					receiverType: "Calculator",
					identifier:   "Calculator.Multiply",
					sig:          "func (c Calculator) Multiply(x, y float64) float64",
				},
			},
		},
		{
			name: "multiple functions",
			source: dedent(`
				// NewProcessor creates a new data processor.
				func NewProcessor() *DataProcessor {
					return &DataProcessor{}
				}

				func (p *DataProcessor) process() {
					// internal processing
				}

				// Export exports the processed data.
				func (p *DataProcessor) Export() []byte {
					return nil
				}
			`),
			wantFuncs: []expectedFunc{
				{name: "NewProcessor", identifier: "NewProcessor", sig: "func NewProcessor() *DataProcessor", hasDoc: true, docContains: "NewProcessor creates"},
				{name: "process", receiverType: "*DataProcessor", identifier: "*DataProcessor.process", sig: "func (p *DataProcessor) process()"},
				{name: "Export", receiverType: "*DataProcessor", identifier: "*DataProcessor.Export", sig: "func (p *DataProcessor) Export() []byte", hasDoc: true, docContains: "Export exports"},
			},
		},
		{
			name: "method with multiline comments",
			source: dedent(`
				/* This is a function,
				   it is important. */
				func (c Calculator) Multiply(x, y float64) float64 { return x * y }
			`),
			wantFuncs: []expectedFunc{
				{
					name:         "Multiply",
					receiverType: "Calculator",
					identifier:   "Calculator.Multiply",
					sig:          "func (c Calculator) Multiply(x, y float64) float64",
					hasDoc:       true,
					docContains:  "This is a function",
					snippet: &expectedSnippet{
						hasExported:      true,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    []byte("/* This is a function,\n   it is important. */\nfunc (c Calculator) Multiply(x, y float64) float64"),
						fullBytes: []byte(dedent(`
							/* This is a function,
							   it is important. */
							func (c Calculator) Multiply(x, y float64) float64 { return x * y }`)),
						docs: []IdentifierDocumentation{
							{Identifier: "Calculator.Multiply", Doc: "/* This is a function,\n   it is important. */\n"},
						},
						missingDocs: nil,
					},
				},
			},
		},
		{
			name: "func with unfortunate block comment inline",
			source: dedent(`
				func foo(x float64) float64 /* boom */ { return x }
			`),
			wantFuncs: []expectedFunc{
				{
					name:         "foo",
					receiverType: "",
					identifier:   "foo",
					sig:          "func foo(x float64) float64",
					hasDoc:       false,
				},
			},
		},
		{
			name: "method with generics receiver",
			source: dedent(`
				func (c Calculator[T]) Multiply(x, y T) T {
					return x * y
				}
			`),
			wantFuncs: []expectedFunc{
				{
					name:         "Multiply",
					receiverType: "Calculator",
					identifier:   "Calculator.Multiply",
					sig:          "func (c Calculator[T]) Multiply(x, y T) T",
				},
			},
		},
		{
			name:   "anonymous function",
			source: `func _() {}`,
			wantFuncs: []expectedFunc{
				{
					name:       "_",
					identifier: "_:test.go:3:6", // col 6: identifier column
					sig:        "func _()",
					snippet: &expectedSnippet{
						hasExported:      false,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    nil,
						fullBytes:        []byte(`func _() {}`),
						docs:             nil,
						missingDocs:      []IdentifierDocumentation{{Identifier: "_:test.go:3:6"}},
					},
				},
			},
		},
		{
			name:   "anonymous method",
			source: "func (f *F) _() {}",
			wantFuncs: []expectedFunc{
				{
					name:         "_",
					identifier:   "*F._:test.go:3:13",
					sig:          "func (f *F) _()",
					receiverType: "*F",
					snippet: &expectedSnippet{
						hasExported:      false,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    nil,
						fullBytes:        []byte(`func (f *F) _() {}`),
						docs:             nil,
						missingDocs:      []IdentifierDocumentation{{Identifier: "*F._:test.go:3:13"}},
					},
				},
			},
		},
		{
			name:   "init function",
			source: "// init\nfunc init() {}",
			wantFuncs: []expectedFunc{
				{
					name:        "init",
					identifier:  "init:test.go:4:6", // col 6: identifier column
					sig:         "func init()",
					hasDoc:      true,
					docContains: "// init",
					snippet: &expectedSnippet{
						hasExported:      false,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    nil,
						fullBytes:        []byte("// init\nfunc init() {}"),
						docs:             []IdentifierDocumentation{{Identifier: "init:test.go:4:6", Doc: "// init\n"}},
						missingDocs:      nil,
					},
				},
			},
		},
		{
			name: "interface method without body",
			source: dedent(`
				// DoSomething performs an action on the implementer.
				// This is an interface method without a body.
				func (i MyInterface) DoSomething(param string) error
			`),
			wantFuncs: []expectedFunc{
				{
					name:         "DoSomething",
					receiverType: "MyInterface",
					identifier:   "MyInterface.DoSomething",
					sig:          "func (i MyInterface) DoSomething(param string) error",
					hasDoc:       true,
					docContains:  "DoSomething performs an action",
					snippet: &expectedSnippet{
						hasExported:      true,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    []byte("// DoSomething performs an action on the implementer.\n// This is an interface method without a body.\nfunc (i MyInterface) DoSomething(param string) error"),
						fullBytes: []byte(dedent(`
							// DoSomething performs an action on the implementer.
							// This is an interface method without a body.
							func (i MyInterface) DoSomething(param string) error
						`)),
						docs: []IdentifierDocumentation{
							{Identifier: "MyInterface.DoSomething", Doc: "// DoSomething performs an action on the implementer.\n// This is an interface method without a body.\n"},
						},
						missingDocs: nil,
					},
				},
			},
		},
		{
			name:   "simple const declaration",
			source: `const MaxConnections = 100`,
			wantValues: []expectedValue{
				{
					identifiers:  []string{"MaxConnections"},
					isVar:        false,
					isBlock:      false,
					snippetBytes: "const MaxConnections = 100",
					snippet: &expectedSnippet{
						hasExported:      true,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    []byte("const MaxConnections = 100"),
						fullBytes:        []byte("const MaxConnections = 100"),
						docs:             nil,
						missingDocs:      []IdentifierDocumentation{{Identifier: "MaxConnections"}},
					},
				},
			},
		},
		{
			name:       "empty blocks",
			source:     "var ()\nconst ()\ntype ()",
			wantValues: []expectedValue{},
		},
		{
			name: "private var declaration",
			source: dedent(`
				// connectionPool is redis
				var connectionPool = "redis"
			`),
			wantValues: []expectedValue{
				{
					identifiers:  []string{"connectionPool"},
					isVar:        true,
					isBlock:      false,
					snippetBytes: "// connectionPool is redis\nvar connectionPool = \"redis\"",
					identifierDocs: map[string]string{
						"connectionPool": "// connectionPool is redis\n",
					},
					snippet: &expectedSnippet{
						hasExported:      false,
						test:             false,
						hasPublicSnippet: false,
						publicSnippet:    nil,
						fullBytes:        []byte("// connectionPool is redis\nvar connectionPool = \"redis\""),
						docs:             []IdentifierDocumentation{{Identifier: "connectionPool", Doc: "// connectionPool is redis\n"}},
						missingDocs:      nil,
					},
				},
			},
		},
		{
			name:   "simple var declaration with eol comment",
			source: `var ConnectionPool = "redis" // ConnectionPool defines the connection pool type`,
			wantValues: []expectedValue{
				{
					identifiers:  []string{"ConnectionPool"},
					isVar:        true,
					isBlock:      false,
					snippetBytes: `var ConnectionPool = "redis" // ConnectionPool defines the connection pool type`,
					identifierDocs: map[string]string{
						"ConnectionPool": "// ConnectionPool defines the connection pool type\n",
					},
				},
			},
		},
		{
			name: "var with both doc and eol comment",
			source: dedent(`
				// timeout for protocol
				var timeout = 1000 // ms
			`),
			wantValues: []expectedValue{
				{
					identifiers:  []string{"timeout"},
					isVar:        true,
					isBlock:      false,
					snippetBytes: "// timeout for protocol\nvar timeout = 1000 // ms",
					identifierDocs: map[string]string{
						"timeout": "// timeout for protocol\n// ms\n",
					},
					snippet: &expectedSnippet{
						hasExported:      false,
						test:             false,
						hasPublicSnippet: false,
						publicSnippet:    nil,
						fullBytes:        []byte("// timeout for protocol\nvar timeout = 1000 // ms"),
						docs:             []IdentifierDocumentation{{Identifier: "timeout", Doc: "// timeout for protocol\n// ms\n"}},
						missingDocs:      nil,
					},
				},
			},
		},
		{
			name: "var block declaration",
			source: dedent(`
				var (
					DefaultTimeout = 30
					MaxRetries     = 3
				)
			`),
			wantValues: []expectedValue{
				{identifiers: []string{"DefaultTimeout", "MaxRetries"}, isVar: true, isBlock: true},
			},
		},
		{
			name: "const block with documentation",
			source: dedent(`
				// Configuration constants for the application.
				const (
					Port int = 8080    // Port defines the server port
					Host     = "localhost"
				)
			`),
			wantValues: []expectedValue{
				{
					identifiers:      []string{"Port", "Host"},
					isVar:            false,
					isBlock:          true,
					hasBlockDoc:      true,
					blockDocContains: "Configuration constants",
					identifierDocs: map[string]string{
						"Port": "// Port defines the server port\n",
					},
				},
			},
		},
		{
			name: "const block with iota and mixed exported/unexported",
			source: dedent(`
				const (
					StatusPending Status = iota // StatusPending represents pending status
					statusProcessing            // unexported status
					StatusComplete              // StatusComplete represents completed status
				)
			`),
			wantValues: []expectedValue{
				{
					identifiers: []string{"StatusPending", "statusProcessing", "StatusComplete"},
					isVar:       false,
					isBlock:     true,
					identifierDocs: map[string]string{
						"StatusPending":    "// StatusPending represents pending status\n",
						"statusProcessing": "// unexported status\n",
						"StatusComplete":   "// StatusComplete represents completed status\n",
					},
					snippet: &expectedSnippet{
						hasExported:      true,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    []byte("const (\n\tStatusPending Status = iota // StatusPending represents pending status\n\n\tStatusComplete // StatusComplete represents completed status\n)"),
						docs: []IdentifierDocumentation{
							{Identifier: "StatusPending", Doc: "// StatusPending represents pending status\n"},
							{Identifier: "statusProcessing", Doc: "// unexported status\n"},
							{Identifier: "StatusComplete", Doc: "// StatusComplete represents completed status\n"},
						},
						missingDocs: nil,
					},
				},
			},
		},
		{
			name:   "single spec var with multiple mixed-exported identifiers",
			source: `var PublicVar, privateVar, AnotherPublic int = 1, 2, 3 // PublicVar and AnotherPublic are exported, privateVar is not`,
			wantValues: []expectedValue{
				{
					identifiers:  []string{"PublicVar", "privateVar", "AnotherPublic"},
					isVar:        true,
					isBlock:      false,
					snippetBytes: `var PublicVar, privateVar, AnotherPublic int = 1, 2, 3 // PublicVar and AnotherPublic are exported, privateVar is not`,
					identifierDocs: map[string]string{
						"PublicVar":     "// PublicVar and AnotherPublic are exported, privateVar is not\n",
						"AnotherPublic": "// PublicVar and AnotherPublic are exported, privateVar is not\n",
						"privateVar":    "// PublicVar and AnotherPublic are exported, privateVar is not\n",
					},
					snippet: &expectedSnippet{
						hasExported:      true,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    []byte("var PublicVar, AnotherPublic int = 1, 3 // PublicVar and AnotherPublic are exported, privateVar is not\n"),
						fullBytes:        []byte(`var PublicVar, privateVar, AnotherPublic int = 1, 2, 3 // PublicVar and AnotherPublic are exported, privateVar is not`),
						docs: []IdentifierDocumentation{
							{Identifier: "PublicVar", Doc: "// PublicVar and AnotherPublic are exported, privateVar is not\n"},
							{Identifier: "privateVar", Doc: "// PublicVar and AnotherPublic are exported, privateVar is not\n"},
							{Identifier: "AnotherPublic", Doc: "// PublicVar and AnotherPublic are exported, privateVar is not\n"},
						},
						missingDocs: nil,
					},
				},
			},
		},
		{
			name:   "simple anonymous var",
			source: `var _ int = 1`,
			wantValues: []expectedValue{
				{
					identifiers:  []string{"_:test.go:3:5"},
					isVar:        true,
					isBlock:      false,
					snippetBytes: `var _ int = 1`,
					snippet: &expectedSnippet{
						hasExported:      false,
						test:             false,
						hasPublicSnippet: false,
						publicSnippet:    nil,
						fullBytes:        []byte(`var _ int = 1`),
						docs:             nil,
						missingDocs:      []IdentifierDocumentation{{Identifier: "_:test.go:3:5"}},
					},
				},
			},
		},
		{
			name: "anonymous var block with docs",
			source: dedent(`
				var (
					// explains first anon var
					_ = "foo"
					// explains second anon var
					_ = "bar"
				)
			`),
			wantValues: []expectedValue{
				{
					identifiers: []string{"_:test.go:5:2", "_:test.go:7:2"},
					isVar:       true,
					isBlock:     true,
					hasBlockDoc: false,
					identifierDocs: map[string]string{
						"_:test.go:5:2": "// explains first anon var\n",
						"_:test.go:7:2": "// explains second anon var\n",
					},
					snippet: &expectedSnippet{
						hasExported:      false,
						test:             false,
						hasPublicSnippet: false,
						publicSnippet:    nil,
						docs: []IdentifierDocumentation{
							{Identifier: "_:test.go:5:2", Doc: "// explains first anon var\n"},
							{Identifier: "_:test.go:7:2", Doc: "// explains second anon var\n"},
						},
						missingDocs: nil,
					},
				},
			},
		},
		{
			name: "const block with mixed exported and unexported, and one spec with multi identifiers",
			source: dedent(`
				// Network configuration constants
				const (
					DefaultPort, maxPort = 8080, 9999 // DefaultPort and maxPort define port range

					// PublicTimeout is the default request timeout
					PublicTimeout        = 30 // seconds
					bufferSize           = 1024
				)
			`),
			wantValues: []expectedValue{
				{
					identifiers:      []string{"DefaultPort", "maxPort", "PublicTimeout", "bufferSize"},
					isVar:            false,
					isBlock:          true,
					hasBlockDoc:      true,
					blockDocContains: "Network configuration constants",
					identifierDocs: map[string]string{
						"DefaultPort":   "// DefaultPort and maxPort define port range\n",
						"maxPort":       "// DefaultPort and maxPort define port range\n",
						"PublicTimeout": "// PublicTimeout is the default request timeout\n// seconds\n",
					},
					snippet: &expectedSnippet{
						hasExported:      true,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    []byte("const (\n\tDefaultPort = 8080 // DefaultPort and maxPort define port range\n\n\t// PublicTimeout is the default request timeout\n\tPublicTimeout = 30 // seconds\n\n)"),
						docs: []IdentifierDocumentation{
							{Identifier: "", Doc: "// Network configuration constants\n"},
							{Identifier: "DefaultPort", Doc: "// DefaultPort and maxPort define port range\n"},
							{Identifier: "maxPort", Doc: "// DefaultPort and maxPort define port range\n"},
							{Identifier: "PublicTimeout", Doc: "// PublicTimeout is the default request timeout\n// seconds\n"},
						},
						missingDocs: []IdentifierDocumentation{
							{Identifier: "bufferSize"},
						},
					},
				},
			},
		},
		{
			name:   "var decl with generic type",
			source: `var M MyType[int]`,
			wantValues: []expectedValue{
				{
					identifiers:  []string{"M"},
					isVar:        true,
					isBlock:      false,
					snippetBytes: "var M MyType[int]",
					snippet: &expectedSnippet{
						hasExported:      true,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    []byte("var M MyType[int]"),
						fullBytes:        []byte("var M MyType[int]"),
						docs:             nil,
						missingDocs:      []IdentifierDocumentation{{Identifier: "M"}},
					},
				},
			},
		},
		{
			name:   "basic type declaration",
			source: `type Foo int`,
			wantTypes: []expectedType{
				{
					identifiers:  []string{"Foo"},
					isBlock:      false,
					snippetBytes: "type Foo int",
					snippet: &expectedSnippet{
						hasExported:      true,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    []byte("type Foo int"),
						fullBytes:        []byte("type Foo int"),
						docs:             nil,
						missingDocs:      []IdentifierDocumentation{{Identifier: "Foo"}},
					},
				},
			},
		},
		{
			name:   "anonymous type",
			source: `type _ int`,
			wantTypes: []expectedType{
				{
					identifiers:  []string{"_:test.go:3:6"},
					isBlock:      false,
					snippetBytes: "type _ int",
					snippet: &expectedSnippet{
						hasExported:      false,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    nil,
						fullBytes:        []byte("type _ int"),
						docs:             nil,
						missingDocs:      []IdentifierDocumentation{{Identifier: "_:test.go:3:6"}},
					},
				},
			},
		},
		{
			name:   "type declaration with end of line comment",
			source: `type Foo int // Foo represents an integer type`,
			wantTypes: []expectedType{
				{
					identifiers:  []string{"Foo"},
					isBlock:      false,
					snippetBytes: "type Foo int // Foo represents an integer type",
					identifierDocs: map[string]string{
						"Foo": "// Foo represents an integer type\n",
					},
				},
			},
		},
		{
			name: "type declaration with doc comment",
			source: dedent(`
				// Foo represents an integer type used for counting.
				type Foo int
			`),
			wantTypes: []expectedType{
				{
					identifiers:  []string{"Foo"},
					isBlock:      false,
					hasBlockDoc:  false,
					snippetBytes: "// Foo represents an integer type used for counting.\ntype Foo int",
					identifierDocs: map[string]string{
						"Foo": "// Foo represents an integer type used for counting.\n",
					},
				},
			},
		},
		{
			name: "type declaration with both doc comment and end of line comment",
			source: dedent(`
				// Foo represents an integer type used for counting.
				type Foo int // Foo is exported for external use
			`),
			wantTypes: []expectedType{
				{
					identifiers: []string{"Foo"},
					isBlock:     false,
					hasBlockDoc: false,
					identifierDocs: map[string]string{
						"Foo": "// Foo represents an integer type used for counting.\n// Foo is exported for external use\n",
					},
					snippetBytes: "// Foo represents an integer type used for counting.\ntype Foo int // Foo is exported for external use",
				},
			},
		},
		{
			name: "block type declaration with several specs",
			source: dedent(`
				// Type definitions for the application
				type (
					// User represents a user in the system
					User struct {
						// Database ID
						ID   int // zero invalid

						// full name
						Name string
						First, Last string
					}

					// status is an internal status type
					status int // zero value is unknown

					// Config holds configuration settings
					Config struct {
						Port int    // Config port
						Host string // Config host
						timeout int
					}

					privateType string
				)
			`),
			wantTypes: []expectedType{
				{
					identifiers:      []string{"User", "status", "Config", "privateType"},
					isBlock:          true,
					hasBlockDoc:      true,
					blockDocContains: "Type definitions for the application",
					identifierDocs: map[string]string{
						"User":   "// User represents a user in the system\n",
						"status": "// status is an internal status type\n// zero value is unknown\n",
						"Config": "// Config holds configuration settings\n",
					},
					fieldDocs: map[string]string{
						"User.ID":        "// Database ID\n// zero invalid\n",
						"User.Name":      "// full name\n",
						"User.First":     "",
						"User.Last":      "",
						"Config.Port":    "// Config port\n",
						"Config.Host":    "// Config host\n",
						"Config.timeout": "",
					},
					snippet: &expectedSnippet{
						hasExported:      true,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    []byte("// Type definitions for the application\ntype (\n\t// User represents a user in the system\n\tUser struct {\n\t\t// Database ID\n\t\tID int // zero invalid\n\n\t\t// full name\n\t\tName        string\n\t\tFirst, Last string\n\t}\n\n\t// Config holds configuration settings\n\tConfig struct {\n\t\tPort int    // Config port\n\t\tHost string // Config host\n\t\t// contains filtered or unexported fields\n\t}\n)"),
						docs: []IdentifierDocumentation{
							{Identifier: "", Doc: "// Type definitions for the application\n"},
							{Identifier: "User", Doc: "// User represents a user in the system\n"},
							{Identifier: "status", Doc: "// status is an internal status type\n// zero value is unknown\n"},
							{Identifier: "Config", Doc: "// Config holds configuration settings\n"},
							{Identifier: "User", Field: "ID", Doc: "// Database ID\n// zero invalid\n"},
							{Identifier: "User", Field: "Name", Doc: "// full name\n"},
							{Identifier: "Config", Field: "Port", Doc: "// Config port\n"},
							{Identifier: "Config", Field: "Host", Doc: "// Config host\n"},
						},
						missingDocs: []IdentifierDocumentation{
							{Identifier: "privateType"},
							{Identifier: "User", Field: "First"},
							{Identifier: "User", Field: "Last"},
							{Identifier: "Config", Field: "timeout"},
						},
					},
				},
			},
		},
		{
			name: "anonymous struct",
			source: dedent(`
				type _ struct {
					Foo int
					Bar int
				}
			`),
			wantTypes: []expectedType{
				{
					identifiers: []string{"_:test.go:3:6"},
					isBlock:     false,
					fieldDocs: map[string]string{
						"_:test.go:3:6.Foo": "",
						"_:test.go:3:6.Bar": "",
					},
					snippet: &expectedSnippet{
						hasExported:      false,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    nil,
						fullBytes: []byte(dedent(`
							type _ struct {
								Foo int
								Bar int
							}
						`)),
						docs: nil,
						missingDocs: []IdentifierDocumentation{
							{Identifier: "_:test.go:3:6"},
							{Identifier: "_:test", Field: "go:3:6.Foo"},
							{Identifier: "_:test", Field: "go:3:6.Bar"},
						},
					},
				},
			},
		},
		{
			name: "nested struct",
			source: dedent(`
				// Type definitions for the application
				type Foo struct {
					// Bar
					Bar struct {
						Baz int // baz
					}
					Qux interface {
						Fizz()
					}
				}
			`),
			wantTypes: []expectedType{
				{
					identifiers:      []string{"Foo"},
					isBlock:          false,
					hasBlockDoc:      false,
					blockDocContains: "",
					identifierDocs: map[string]string{
						"Foo": "// Type definitions for the application\n",
					},
					fieldDocs: map[string]string{
						"Foo.Bar":      "// Bar\n",
						"Foo.Bar.Baz":  "// baz\n",
						"Foo.Qux":      "",
						"Foo.Qux.Fizz": "",
					},
					snippet: &expectedSnippet{
						hasExported:      true,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    []byte("// Type definitions for the application\ntype Foo struct {\n\t// Bar\n\tBar struct {\n\t\tBaz int // baz\n\t}\n\tQux interface {\n\t\tFizz()\n\t}\n}"),
						docs: []IdentifierDocumentation{
							{Identifier: "Foo", Field: "", Doc: "// Type definitions for the application\n"},
							{Identifier: "Foo", Field: "Bar", Doc: "// Bar\n"},
							{Identifier: "Foo", Field: "Bar.Baz", Doc: "// baz\n"},
						},
						missingDocs: []IdentifierDocumentation{
							{Identifier: "Foo", Field: "Qux"},
							{Identifier: "Foo", Field: "Qux.Fizz"},
						},
					},
				},
			},
		},
		{
			name: "embedded struct",
			source: dedent(`
				// Type definitions for the application
				type Foo struct {
					// bar
					Bar
					pkg.Baz // baz
					privateStruct
				}
			`),
			wantTypes: []expectedType{
				{
					identifiers:      []string{"Foo"},
					isBlock:          false,
					hasBlockDoc:      false,
					blockDocContains: "",
					identifierDocs: map[string]string{
						"Foo": "// Type definitions for the application\n",
					},
					fieldDocs: map[string]string{
						"Foo.Bar":           "// bar\n",
						"Foo.pkg.Baz":       "// baz\n",
						"Foo.privateStruct": "",
					},
					snippet: &expectedSnippet{
						hasExported:      true,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    []byte("// Type definitions for the application\ntype Foo struct {\n\t// bar\n\tBar\n\tpkg.Baz // baz\n\t// contains filtered or unexported fields\n}"),
						docs: []IdentifierDocumentation{
							{Identifier: "Foo", Field: "", Doc: "// Type definitions for the application\n"},
							{Identifier: "Foo", Field: "Bar", Doc: "// bar\n"},
							{Identifier: "Foo", Field: "pkg.Baz", Doc: "// baz\n"},
						},
						missingDocs: []IdentifierDocumentation{
							{Identifier: "Foo", Field: "privateStruct"},
						},
					},
				},
			},
		},
		{
			name: "type declaration - parameterized",
			source: dedent(`
				type Number interface {
					~int | ~float
				}
				type Vector[T Number] struct {
					X, Y T
				}
			`),
			wantTypes: []expectedType{
				{
					identifiers: []string{"Number"},
					snippet: &expectedSnippet{
						hasExported:      true,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    []byte("type Number interface {\n\t~int | ~float\n}"),
						docs:             nil,
						missingDocs:      []IdentifierDocumentation{{Identifier: "Number"}},
					},
				},
				{
					identifiers: []string{"Vector"},
					snippet: &expectedSnippet{
						hasExported:      true,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    []byte("type Vector[T Number] struct {\n\tX, Y T\n}"),
						docs:             nil,
						missingDocs: []IdentifierDocumentation{
							{Identifier: "Vector"},
							{Identifier: "Vector", Field: "X"},
							{Identifier: "Vector", Field: "Y"},
						},
					},
				},
			},
		},
		{
			name: "interface",
			source: dedent(`
				type Foo interface {
					// Bar
					Bar()
					Baz() int // Baz
					qux()
					Embedded
				}
			`),
			wantTypes: []expectedType{
				{
					identifiers: []string{"Foo"},
					isBlock:     false,
					fieldDocs: map[string]string{
						"Foo.Bar":      "// Bar\n",
						"Foo.Baz":      "// Baz\n",
						"Foo.qux":      "",
						"Foo.Embedded": "",
					},
					snippet: &expectedSnippet{
						hasExported:      true,
						test:             false,
						hasPublicSnippet: true,
						publicSnippet:    []byte("type Foo interface {\n\t// Bar\n\tBar()\n\tBaz() int // Baz\n\n\tEmbedded\n\t// contains filtered or unexported methods\n}"),
						docs: []IdentifierDocumentation{
							{Identifier: "Foo", Field: "Bar", Doc: "// Bar\n"},
							{Identifier: "Foo", Field: "Baz", Doc: "// Baz\n"},
						},
						missingDocs: []IdentifierDocumentation{
							{Identifier: "Foo"},
							{Identifier: "Foo", Field: "qux"},
							{Identifier: "Foo", Field: "Embedded"},
						},
					},
				},
			},
		},
		{
			name: "package docs basic",
			source: dedent(`
				// Package docs
				package foo
			`),
			wantPackageDoc: &expectedPackageDoc{
				hasDoc:       true,
				docs:         "// Package docs\n",
				snippetBytes: "// Package docs\npackage foo",
				snippet: &expectedSnippet{
					hasExported:      true,
					test:             false,
					hasPublicSnippet: true,
					publicSnippet:    []byte("// Package docs\npackage foo"),
					docs: []IdentifierDocumentation{
						{Identifier: "package:test.go", Field: "", Doc: "// Package docs\n"},
					},
					missingDocs: nil,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			funcSnippets, valueSnippets, typeSnippets, packageDocSnippet, err := extractSnippetsFromSource(t, tt.source)
			require.NoError(t, err, "extractSnippets should not return an error")

			assert.Len(t, funcSnippets, len(tt.wantFuncs))
			assert.Len(t, valueSnippets, len(tt.wantValues))
			assert.Len(t, typeSnippets, len(tt.wantTypes))

			// Check package doc expectations
			if tt.wantPackageDoc != nil {
				require.NotNil(t, packageDocSnippet, "Expected package doc snippet")
				assertPackageDocSnippet(t, packageDocSnippet, *tt.wantPackageDoc)
			} else {
				assert.Nil(t, packageDocSnippet, "Expected no package doc snippet")
			}

			for i, want := range tt.wantFuncs {
				if !assert.Less(t, i, len(funcSnippets)) {
					continue
				}
				assertFuncSnippet(t, funcSnippets[i], want)
			}

			for i, want := range tt.wantValues {
				if !assert.Less(t, i, len(valueSnippets)) {
					continue
				}
				assertValueSnippet(t, valueSnippets[i], want)
			}

			for i, want := range tt.wantTypes {
				if !assert.Less(t, i, len(typeSnippets)) {
					continue
				}
				assertTypeSnippet(t, typeSnippets[i], want)
			}
		})
	}
}

func TestExtractFuncSnippet(t *testing.T) {
	source := dedent(`
		// ProcessData processes the input data and returns the result.
		// This is a multi-line comment that explains the function behavior.
		func (p *DataProcessor) ProcessData(input string, count int) (string, error) {
			if input == "" {
				return "", fmt.Errorf("empty input")
			}
			return strings.Repeat(input, count), nil
		}
	`)

	funcSnippets, _, _, _, err := extractSnippetsFromSource(t, source)
	require.NoError(t, err, "extractSnippets should not return an error")

	require.Len(t, funcSnippets, 1, "Expected exactly one function snippet")

	snippet := funcSnippets[0]

	// Test all FuncSnippet fields
	assert.Equal(t, "ProcessData", snippet.Name, "Name field mismatch")
	assert.Equal(t, "*DataProcessor", snippet.ReceiverType, "ReceiverType field mismatch")
	assert.Equal(t, "*DataProcessor.ProcessData", snippet.Identifier, "Identifier field mismatch")
	assert.Equal(t, "func (p *DataProcessor) ProcessData(input string, count int) (string, error)", snippet.Sig, "Sig field mismatch")
	assert.Equal(t, "test.go", snippet.FileName, "FileName field mismatch")

	// Test Doc field - should contain the documentation with comment markers and be newline terminated
	expectedDoc := "// ProcessData processes the input data and returns the result.\n// This is a multi-line comment that explains the function behavior.\n"
	assert.Equal(t, expectedDoc, snippet.Doc, "Doc field mismatch")
	assert.True(t, strings.HasSuffix(snippet.Doc, "\n"), "Doc should be newline terminated")

	// Test Snippet field - should contain the docs and function signature up to but not including opening brace
	expectedSnippet := strings.TrimSpace(dedent(`
		// ProcessData processes the input data and returns the result.
		// This is a multi-line comment that explains the function behavior.
		func (p *DataProcessor) ProcessData(input string, count int) (string, error)
	`))
	assert.Equal(t, expectedSnippet, string(snippet.Snippet), "Snippet field should exactly match expected content")

	// Test FullFunc field - should contain the entire function including body
	expectedFullFunc := strings.TrimSpace(source)
	assert.Equal(t, expectedFullFunc, string(snippet.FullFunc), "FullFunc field should contain the entire function including body")

	// Test Position method
	pos := snippet.Position()
	assert.Equal(t, "test.go", pos.Filename)
	assert.Equal(t, 3, pos.Line, "Line should be start of doc comment")
	assert.Equal(t, 1, pos.Column)
}

func TestExtractValueSnippet(t *testing.T) {
	source := dedent(`
		// MaxRetries is the maximum number of retry attempts.
		// This is used by the retry mechanism to limit attempts.
		const (
			MaxRetries int = 3    // MaxRetries defines the retry limit
			Timeout        = 30   // Timeout defines the default timeout in seconds
		)
	`)

	funcSnippets, valueSnippets, typeSnippets, _, err := extractSnippetsFromSource(t, source)
	require.NoError(t, err, "extractSnippets should not return an error")

	// Verify only value snippets are extracted (no functions or types in this test)
	assert.Nil(t, funcSnippets, "Function snippets should be nil")
	assert.Nil(t, typeSnippets, "Type snippets should be nil")
	require.Len(t, valueSnippets, 1, "Expected exactly one value snippet")

	snippet := valueSnippets[0]

	// Test all ValueSnippet fields
	expectedIdentifiers := []string{"MaxRetries", "Timeout"}
	assert.Equal(t, expectedIdentifiers, snippet.Identifiers, "Identifiers field mismatch")
	assert.False(t, snippet.IsVar, "IsVar should be false for const declaration")
	assert.True(t, snippet.IsBlock, "IsBlock should be true for const block")
	assert.Equal(t, "test.go", snippet.FileName, "FileName field mismatch")

	// Test BlockDoc field - should contain the documentation with comment markers and be newline terminated
	expectedBlockDoc := "// MaxRetries is the maximum number of retry attempts.\n// This is used by the retry mechanism to limit attempts.\n"
	assert.Equal(t, expectedBlockDoc, snippet.BlockDoc, "BlockDoc field mismatch")

	// Test IdentifierDocs field - should contain docs for individual identifiers
	expectedDocs := map[string]string{
		"MaxRetries": "// MaxRetries defines the retry limit\n",
		"Timeout":    "// Timeout defines the default timeout in seconds\n",
	}
	assert.Equal(t, expectedDocs, snippet.IdentifierDocs, "IdentifierDocs field mismatch")

	// Test Snippet field - should contain the docs and entire declaration
	expectedSnippet := strings.TrimSpace(source)
	assert.Equal(t, expectedSnippet, string(snippet.Snippet), "Snippet field should exactly match expected content")

	// Test Position method
	pos := snippet.Position()
	assert.Equal(t, "test.go", pos.Filename)
	assert.Equal(t, 3, pos.Line, "Line should be start of doc comment")
	assert.Equal(t, 1, pos.Column)
}

func TestExtractTypeSnippet(t *testing.T) {
	source := dedent(`
		// User represents a system user.
		// This is used throughout the application for authentication.
		type (
			User struct {
				ID   int    // User ID for database reference
				Name string // User display name
			}
		)
	`)

	funcSnippets, valueSnippets, typeSnippets, _, err := extractSnippetsFromSource(t, source)
	require.NoError(t, err, "extractSnippets should not return an error")

	// Verify only type snippets are extracted (no functions or values in this test)
	assert.Nil(t, funcSnippets, "Function snippets should be nil")
	assert.Nil(t, valueSnippets, "Value snippets should be nil")
	require.Len(t, typeSnippets, 1, "Expected exactly one type snippet")

	snippet := typeSnippets[0]

	// Test all TypeSnippet fields
	expectedIdentifiers := []string{"User"}
	assert.Equal(t, expectedIdentifiers, snippet.Identifiers, "Identifiers field mismatch")
	assert.True(t, snippet.IsBlock, "IsBlock should be true for type block")
	assert.Equal(t, "test.go", snippet.FileName, "FileName field mismatch")

	// Test BlockDoc field - should contain the documentation with comment markers and be newline terminated
	expectedBlockDoc := "// User represents a system user.\n// This is used throughout the application for authentication.\n"
	assert.Equal(t, expectedBlockDoc, snippet.BlockDoc, "BlockDoc field mismatch")

	// Test FieldDocs field - should contain documentation for struct fields
	expectedFieldDocs := map[string]string{
		"User.ID":   "// User ID for database reference\n",
		"User.Name": "// User display name\n",
	}
	assert.Equal(t, expectedFieldDocs, snippet.FieldDocs, "FieldDocs field mismatch")

	// Test Snippet field - should contain the docs and entire declaration
	expectedSnippet := strings.TrimSpace(source)
	assert.Equal(t, expectedSnippet, string(snippet.Snippet), "Snippet field should exactly match expected content")

	// Test Position method
	pos := snippet.Position()
	assert.Equal(t, "test.go", pos.Filename)
	assert.Equal(t, 3, pos.Line, "Line should be start of doc comment")
	assert.Equal(t, 1, pos.Column)
}

func TestIDIsDocumented(t *testing.T) {
	tests := []struct {
		name              string
		source            string // Go source code (without package declaration)
		identifier        string // Identifier to check within the snippet
		blockDocsAllSpecs bool   // Whether to treat block documentation as applying to all specs
		wantAny           bool   // Expected value for anyDocs
		wantFull          bool   // Expected value for fullDocs
	}{
		{
			name:       "function with documentation",
			source:     "// Foo does something.\nfunc Foo() {}",
			identifier: "Foo",
			wantAny:    true,
			wantFull:   true,
		},
		{
			name:       "function without documentation",
			source:     "func Foo() {}",
			identifier: "Foo",
			wantAny:    false,
			wantFull:   false,
		},
		{
			name:       "single var with documentation",
			source:     "// A is a variable\nvar A = 1",
			identifier: "A",
			wantAny:    true,
			wantFull:   true,
		},
		{
			name:       "single var without documentation",
			source:     "var A = 1",
			identifier: "A",
			wantAny:    false,
			wantFull:   false,
		},
		{
			name:              "const block with block docs - blockDocsAllSpecs=false",
			source:            "// Constants for demo\nconst (\n    A = 1\n    B = 2\n)",
			identifier:        "A",
			blockDocsAllSpecs: false,
			wantAny:           true,
			wantFull:          false,
		},
		{
			name:              "const block with block docs - blockDocsAllSpecs=true",
			source:            "// Constants for demo\nconst (\n    A = 1\n    B = 2\n)",
			identifier:        "A",
			blockDocsAllSpecs: true,
			wantAny:           true,
			wantFull:          true,
		},
		{
			name:              "const block with no block docs - blockDocsAllSpecs=true, but spec is documented",
			source:            "const (\n    // A ...\n    A = 1\n    B = 2\n)",
			identifier:        "A",
			blockDocsAllSpecs: true,
			wantAny:           true,
			wantFull:          true,
		},
		{
			name:       "struct with missing field docs",
			source:     "// Foo is a structure.\ntype Foo struct {\n    // A is documented\n    A int\n    B string\n}",
			identifier: "Foo",
			wantAny:    true,
			wantFull:   false,
		},
		{
			name:       "struct with all field docs",
			source:     "// Foo is a structure.\ntype Foo struct {\n    // A is documented\n    A int\n    // B is also documented\n    B string\n}",
			identifier: "Foo",
			wantAny:    true,
			wantFull:   true,
		},
		{
			name:              "type block with struct with all field docs - block doc'ed and blockDocsAllSpecs",
			source:            "// Types.\ntype (\n    Foo struct {\n    // A is documented\n    A int\n    // B is also documented\n    B string\n}\n)",
			identifier:        "Foo",
			blockDocsAllSpecs: true,
			wantAny:           true,
			wantFull:          true,
		},
		{
			name:              "type block with struct with all field docs - block doc'ed, and !blockDocsAllSpecs",
			source:            "// Types.\ntype (\n    Foo struct {\n    // A is documented\n    A int\n    // B is also documented\n    B string\n}\n)",
			identifier:        "Foo",
			blockDocsAllSpecs: false,
			wantAny:           true,
			wantFull:          false,
		},
		{
			name:              "type block with struct with missing field",
			source:            "// Types.\ntype (\n    Foo struct {\n    A int\n    // B is also documented\n    B string\n}\n)",
			identifier:        "Foo",
			blockDocsAllSpecs: true,
			wantAny:           true,
			wantFull:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Extract snippets from the provided source.
			funcSnips, valueSnips, typeSnips, _, err := extractSnippetsFromSource(t, tt.source)
			require.NoError(t, err, "extractSnippetsFromSource should not error")

			// Helper to find the snippet that contains the identifier under test.
			findSnippet := func() Snippet {
				// Search function snippets first.
				for _, s := range funcSnips {
					for _, id := range s.IDs() {
						if id == tt.identifier {
							return s
						}
					}
				}
				// Search value snippets.
				for _, s := range valueSnips {
					for _, id := range s.IDs() {
						if id == tt.identifier {
							return s
						}
					}
				}
				// Search type snippets.
				for _, s := range typeSnips {
					for _, id := range s.IDs() {
						if id == tt.identifier {
							return s
						}
					}
				}
				return nil
			}

			snippet := findSnippet()
			require.NotNil(t, snippet, "snippet containing identifier %q not found", tt.identifier)

			gotAny, gotFull := IDIsDocumented(snippet, tt.identifier, tt.blockDocsAllSpecs)

			assert.Equal(t, tt.wantAny, gotAny, "anyDocs mismatch")
			assert.Equal(t, tt.wantFull, gotFull, "fullDocs mismatch")
		})
	}
}
