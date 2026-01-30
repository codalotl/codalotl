package gocode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMarshalInSitu(t *testing.T) {
	m, err := NewModule(MustCwd())
	assert.NoError(t, err)

	err = m.LoadAllPackages()
	assert.NoError(t, err)

	p, err := m.LoadPackageByRelativeDir("internal/gocode")
	require.NoError(t, err)
	require.NotNil(t, p)

	b, err := p.MarshalDocumentation()
	assert.NoError(t, err)

	str := string(b)

	// Uncomment to see docs
	// fmt.Println(str)

	// This is just a quick sanity check. We don't want to add too much here so that editing this package doesn't get too brittle.
	assert.Contains(t, str, "// Types and their methods")
	assert.Contains(t, str, "type Package struct {")
	assert.Contains(t, str, "func (p *Package) FileNames() []string")
	assert.Contains(t, str, "func (p *Package) GetSnippet(identifier string) Snippet")
	assert.NotContains(t, str, "TestParseMarshalInSitu")
	assert.NotContains(t, str, "extractSnippets")
}

// TestParseIdempotence verifies that calling Parse multiple times is idempotent and does not change the results or cause errors.
func TestParseIdempotence(t *testing.T) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "gopackage-parse-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create a simple Go file with exportable content
	codeContent := dedent(`
		package testpkg

		// ExportedStruct is a struct for testing Parse idempotence
		type ExportedStruct struct {
			Field string // exported field
		}

		// ExportedFunc does something useful
		func ExportedFunc() string {
			return "hello"
		}

		// ExportedConst is a constant
		const ExportedConst = "constant value"
	`)
	testFile := filepath.Join(tempDir, "main.go")
	err = os.WriteFile(testFile, []byte(codeContent), 0644)
	assert.NoError(t, err)

	// Create a module for the test
	module := &Module{
		Name:         "testmodule",
		AbsolutePath: tempDir,
		Packages:     make(map[string]*Package),
	}

	// Create a new package
	pkg, err := NewPackage("", tempDir, []string{"main.go"}, module)
	assert.NoError(t, err)
	assert.NotNil(t, pkg)
	assert.Equal(t, module, pkg.Module)
	assert.True(t, pkg.parsed)

	// Capture state after first parse
	firstFuncsCount := len(pkg.FuncSnippets)
	firstTypesCount := len(pkg.TypeSnippets)
	firstValuesCount := len(pkg.ValueSnippets)
	assert.Greater(t, firstFuncsCount, 0, "Should have parsed at least one function")
	assert.Greater(t, firstTypesCount, 0, "Should have parsed at least one type")
	assert.Greater(t, firstValuesCount, 0, "Should have parsed at least one value")

	// Second parse
	err = pkg.parse()
	assert.NoError(t, err)
	assert.True(t, pkg.parsed, "Package should still be marked as parsed after second Parse call")

	// Verify data didn't change
	assert.Equal(t, firstFuncsCount, len(pkg.FuncSnippets), "Number of exported functions should not change")
	assert.Equal(t, firstTypesCount, len(pkg.TypeSnippets), "Number of exported types should not change")
	assert.Equal(t, firstValuesCount, len(pkg.ValueSnippets), "Number of exported values should not change")

	// Verify content consistency between first and second parse
	if len(pkg.FuncSnippets) > 0 {
		assert.Equal(t, "ExportedFunc", pkg.FuncSnippets[0].Name, "Function name should remain the same")
	}
	if len(pkg.TypeSnippets) > 0 {
		assert.Equal(t, "ExportedStruct", pkg.TypeSnippets[0].Identifiers[0], "Type name should remain the same")
	}
	if len(pkg.ValueSnippets) > 0 {
		assert.Contains(t, pkg.ValueSnippets[0].Identifiers, "ExportedConst", "Value identifiers should contain constant name")
	}
}

func TestNewPackage(t *testing.T) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "gopackage-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create a simple Go file in the temporary directory
	mainFile := filepath.Join(tempDir, "main.go")
	err = os.WriteFile(mainFile, []byte("package foo\n\nfunc main() {}\n"), 0644)
	assert.NoError(t, err)

	// Create a test file in the same directory
	testFile := filepath.Join(tempDir, "main_test.go")
	err = os.WriteFile(testFile, []byte("package foo\n\nfunc TestMain(t *testing.T) {}\n"), 0644)
	assert.NoError(t, err)

	// Create a module for the test
	module := &Module{
		Name:         "testmodule",
		AbsolutePath: tempDir,
		Packages:     make(map[string]*Package),
	}

	// Create a package with the files
	pkg, err := NewPackage("", tempDir, []string{"main.go", "main_test.go"}, module)
	assert.NoError(t, err)
	assert.NotNil(t, pkg)
	assert.Equal(t, module, pkg.Module)

	// Verify the package properties
	assert.Equal(t, "foo", pkg.Name)
	assert.Equal(t, "", pkg.RelativeDir)
	assert.Equal(t, "testmodule", pkg.ImportPath)
	assert.False(t, pkg.HasTestPackage()) // No test package because test file uses same package name

	// Verify the files were added to the package
	assert.Len(t, pkg.Files, 2)
	assert.Contains(t, pkg.Files, "main.go")
	assert.Contains(t, pkg.Files, "main_test.go")

	f, hasFile := pkg.Files["main.go"]
	if assert.True(t, hasFile) {
		assert.Equal(t, "main.go", f.FileName)
		assert.Equal(t, false, f.IsTest)
		assert.Equal(t, "foo", f.PackageName)
		assert.Equal(t, "main.go", f.RelativeFileName)
		assert.Contains(t, f.AbsolutePath, "main.go")
		assert.True(t, filepath.IsAbs(f.AbsolutePath))
		assert.Contains(t, string(f.Contents), "func main")
	}

	// Create a new test file with a different package name
	blackBoxTestFile := filepath.Join(tempDir, "blackbox_test.go")
	err = os.WriteFile(blackBoxTestFile, []byte("package foo_test\n\nfunc TestBlackBox(t *testing.T) {}\n"), 0644)
	assert.NoError(t, err)

	// Create a package with all files including the black-box test
	pkg, err = NewPackage("", tempDir, []string{"main.go", "main_test.go", "blackbox_test.go"}, module)
	assert.NoError(t, err)
	assert.NotNil(t, pkg)
	assert.Equal(t, module, pkg.Module)

	// Verify the package properties
	assert.Equal(t, "foo", pkg.Name)
	assert.Equal(t, "", pkg.RelativeDir)
	assert.Equal(t, "testmodule", pkg.ImportPath)
	assert.True(t, pkg.HasTestPackage()) // Has test package because black-box test uses different package name

	// Verify main package files only contain non-black-box test files
	assert.Len(t, pkg.Files, 2)
	assert.Contains(t, pkg.Files, "main.go")
	assert.Contains(t, pkg.Files, "main_test.go")
	assert.NotContains(t, pkg.Files, "blackbox_test.go")

	// Verify test package contains the black-box test file
	assert.NotNil(t, pkg.TestPackage)
	assert.Equal(t, "foo_test", pkg.TestPackage.Name)
	assert.Len(t, pkg.TestPackage.Files, 1)
	assert.Contains(t, pkg.TestPackage.Files, "blackbox_test.go")
}

func TestTypeParsing(t *testing.T) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "gopackage-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name                 string
		code                 string
		wantType             *TypeSnippet
		wantErr              bool
		wantCountDiffFromOne int // 0: we want 1 type; -1: we want 0 types; 2: we want 3 types, etc
		checkTypeIdx         int // 1: we will check that tye 2nd type is wantType; -1: no checking
	}{
		{
			name: "simple struct",
			code: dedent(`
				// MyStruct is a test struct
				type MyStruct struct {
					// Field1 is an exported field
					Field1 string
					// field2 is unexported
					field2 int
				}`),
			wantType: &TypeSnippet{
				Identifiers: []string{"MyStruct"},
				IsBlock:     false,
				IdentifierDocs: map[string]string{
					"MyStruct": "// MyStruct is a test struct\n",
				},
			},
		},
		{
			name: "unexported",
			code: dedent(`
				// MyStruct is a test struct
				type myStruct struct {
					// Field1 is an exported field
					Field1 string
					// field2 is unexported
					field2 int
				}`),
			wantType: &TypeSnippet{
				Identifiers: []string{"myStruct"},
				IsBlock:     false,
				IdentifierDocs: map[string]string{
					"myStruct": "// MyStruct is a test struct\n",
				},
			},
			wantCountDiffFromOne: 0, // we still parse unexported items, they just don't appear in public docs
			checkTypeIdx:         0,
		},
		{
			name: "single type in type block",
			code: dedent(`
				// Outter comment
				type (
					// Inner comment
					MyStruct struct {
						// Above-field comment
						Field string // field comment
						// Below-field comment
					}
				)`),
			wantType: &TypeSnippet{
				Identifiers: []string{"MyStruct"},
				IsBlock:     true,
				BlockDoc:    "// Outter comment\n",
				IdentifierDocs: map[string]string{
					"MyStruct": "// Inner comment\n",
				},
			},
		},
		{
			name: "multi type in type block - first struct",
			code: dedent(`
				// Outter comment
				type (
					// Inner comment1
					MyStruct1 struct {
						Field string // field comment
					}

					// Inner comment2
					MyStruct2 struct {
						Field string // field comment
					}
				)`),
			wantType: &TypeSnippet{
				Identifiers: []string{"MyStruct1", "MyStruct2"},
				IsBlock:     true,
				BlockDoc:    "// Outter comment\n",
				IdentifierDocs: map[string]string{
					"MyStruct1": "// Inner comment1\n",
					"MyStruct2": "// Inner comment2\n",
				},
			},
			wantCountDiffFromOne: 0,
			checkTypeIdx:         0,
		},
		{
			name: "multi type in type block - second struct",
			code: dedent(`
				// Outter comment
				type (
					// Inner comment1
					MyStruct1 struct {
						Field string // field comment
					}

					// Inner comment2
					MyStruct2 struct {
						Field string // field comment
					}
				)`),
			wantType: &TypeSnippet{
				Identifiers: []string{"MyStruct1", "MyStruct2"},
				IsBlock:     true,
				BlockDoc:    "// Outter comment\n",
				IdentifierDocs: map[string]string{
					"MyStruct1": "// Inner comment1\n",
					"MyStruct2": "// Inner comment2\n",
				},
			},
			wantCountDiffFromOne: 0,
			checkTypeIdx:         0,
		},
		{
			name: "simple interface",
			code: dedent(`
				// Foo does foolish things
				//
				// It is great.
				type MyInterface interface {
					// does foo
					Foo() int
				}`),
			wantType: &TypeSnippet{
				Identifiers: []string{"MyInterface"},
				IsBlock:     false,
				IdentifierDocs: map[string]string{
					"MyInterface": "// Foo does foolish things\n//\n// It is great.\n",
				},
			},
		},
		{
			name: "simple interface with unexported field",
			code: dedent(`
				type MyInterface interface {
					Foo() int
					bar()
				}`),
			wantType: &TypeSnippet{
				Identifiers: []string{"MyInterface"},
				IsBlock:     false,
				IdentifierDocs: map[string]string{
					"MyInterface": "",
				},
			},
		},

		{
			name: "an int",
			code: dedent(`
				// Comment
				type MyInt int`),
			wantType: &TypeSnippet{
				Identifiers: []string{"MyInt"},
				IsBlock:     false,
				IdentifierDocs: map[string]string{
					"MyInt": "// Comment\n",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Make sure we formed the test-case correctly (if we check an index, we expect the # of types to be sufficient)
			assert.True(t, tc.checkTypeIdx <= tc.wantCountDiffFromOne)

			// Write the test code to a temporary file
			testFile := filepath.Join(tempDir, "test.go")
			err := os.WriteFile(testFile, []byte("package foo\n\n"+tc.code), 0644)
			assert.NoError(t, err)

			// Create a module for the test
			module := &Module{
				Name:         "testmodule",
				AbsolutePath: tempDir,
				Packages:     make(map[string]*Package),
			}

			// Create a package with the test file
			pkg, err := NewPackage("", tempDir, []string{"test.go"}, module)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, pkg)
			assert.Equal(t, module, pkg.Module)

			// Verify we got the expected type
			assert.Len(t, pkg.TypeSnippets, tc.wantCountDiffFromOne+1)
			if tc.checkTypeIdx >= 0 {
				if assert.True(t, tc.checkTypeIdx < len(pkg.TypeSnippets)) {
					gotType := pkg.TypeSnippets[tc.checkTypeIdx]
					assert.Equal(t, tc.wantType.Identifiers, gotType.Identifiers)
					assert.Equal(t, tc.wantType.IsBlock, gotType.IsBlock)
					assert.Equal(t, tc.wantType.BlockDoc, gotType.BlockDoc)
					assert.Equal(t, tc.wantType.IdentifierDocs, gotType.IdentifierDocs)
					assert.Equal(t, "test.go", gotType.FileName)
				}
			}
		})
	}
}

func TestFunctionParsing(t *testing.T) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "gopackage-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name     string
		code     string
		wantFunc *FuncSnippet
		wantErr  bool
	}{
		{
			name: "simple function",
			code: dedent(`
				// DoSomething performs an important task
				//
				// with multiple lines of documentation
				func DoSomething(input string) (string, error) {
					return input + " processed", nil
				}`),
			wantFunc: &FuncSnippet{
				Name:         "DoSomething",
				ReceiverType: "",
				Doc:          "// DoSomething performs an important task\n//\n// with multiple lines of documentation\n",
				Sig:          "func DoSomething(input string) (string, error)",
			},
		},
		{
			name: "method with pointer receiver",
			code: dedent(`
				// MyType is a sample type
				type MyType struct {
					Value string
				}

				// Process handles the input and returns a result
				//
				// It's a method on *MyType
				func (t *MyType) Process(input int) bool {
					return len(t.Value) > input
				}`),
			wantFunc: &FuncSnippet{
				Name:         "Process",
				ReceiverType: "*MyType",
				Doc:          "// Process handles the input and returns a result\n//\n// It's a method on *MyType\n",
				Sig:          "func (t *MyType) Process(input int) bool",
			},
		},
		{
			name: "exported method on unexported type is parsed but not exported",
			code: dedent(`
				type myType struct{}

				func (m myType) Visible() {}
			`),
			wantFunc: &FuncSnippet{
				Name:         "Visible",
				ReceiverType: "myType",
				Doc:          "",
				Sig:          "func (m myType) Visible()",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Write the test code to a temporary file
			testFile := filepath.Join(tempDir, "test.go")
			err := os.WriteFile(testFile, []byte("package foo\n\n"+tc.code), 0644)
			assert.NoError(t, err)

			// Create a module for the test
			module := &Module{
				Name:         "testmodule",
				AbsolutePath: tempDir,
				Packages:     make(map[string]*Package),
			}

			// Create a package with the test file
			pkg, err := NewPackage("", tempDir, []string{"test.go"}, module)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, pkg)

			// Verify we got the expected function(s)
			if tc.wantFunc == nil {
				assert.Len(t, pkg.FuncSnippets, 0)
				return
			}

			// Find the function we're testing (there might be multiple if we defined a type too)
			var gotFunc *FuncSnippet
			for _, f := range pkg.FuncSnippets {
				if f.Name == tc.wantFunc.Name {
					gotFunc = f
					break
				}
			}

			if assert.NotNil(t, gotFunc, "Expected function %s not found", tc.wantFunc.Name) {
				assert.Equal(t, tc.wantFunc.Name, gotFunc.Name)
				assert.Equal(t, tc.wantFunc.ReceiverType, gotFunc.ReceiverType)
				assert.Equal(t, tc.wantFunc.Doc, gotFunc.Doc)
				assert.Equal(t, tc.wantFunc.Sig, gotFunc.Sig)
				assert.Equal(t, "test.go", gotFunc.FileName)
			}
		})
	}
}

func TestValueParsing(t *testing.T) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "gopackage-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name                 string
		code                 string
		wantValue            *ValueSnippet
		wantErr              bool
		wantCountDiffFromOne int // 0: we want 1 type; -1: we want 0 types; 2: we want 3 types, etc
	}{
		{
			name: "const with comment",
			code: dedent(`
				// DefaultTimeout represents the standard operation timeout in seconds
				const DefaultTimeout = 30`),
			wantValue: &ValueSnippet{
				Identifiers: []string{"DefaultTimeout"},
				IsVar:       false,
				IsBlock:     false,
				IdentifierDocs: map[string]string{
					"DefaultTimeout": "// DefaultTimeout represents the standard operation timeout in seconds\n",
				},
			},
		},
		{
			name: "var with comment",
			code: dedent(`
				// Foo
				//
				// bar
				var DefaultTimeout = 30`),
			wantValue: &ValueSnippet{
				Identifiers: []string{"DefaultTimeout"},
				IsVar:       true,
				IsBlock:     false,
				IdentifierDocs: map[string]string{
					"DefaultTimeout": "// Foo\n//\n// bar\n",
				},
			},
		},
		{
			name: "const block",
			code: dedent(`
				// Various constants
				const (
					TopicFoo int = iota
					TopicBar // bar
					Topicıaz
				)`),
			wantValue: &ValueSnippet{
				Identifiers: []string{"TopicFoo", "TopicBar", "Topicıaz"},
				IsVar:       false,
				IsBlock:     true,
				BlockDoc:    "// Various constants\n",
			},
		},
		{
			name: "mixed exporting in var block",
			code: dedent(`
				// Various vars
				var (
					Var1 int
					var2 int
					Var3 int
				)`),
			wantValue: &ValueSnippet{
				Identifiers: []string{"Var1", "var2", "Var3"},
				IsVar:       true,
				IsBlock:     true,
				BlockDoc:    "// Various vars\n",
			},
		},
		{
			name: "nothing exported",
			code: dedent(`
				// private consts
				const (
					c1 int
					c2 int
				)`),
			wantValue: &ValueSnippet{
				Identifiers: []string{"c1", "c2"},
				IsVar:       false,
				IsBlock:     true,
				BlockDoc:    "// private consts\n",
			},
			wantCountDiffFromOne: 0, // we still parse unexported items, they just don't appear in public docs
		},
		{
			name: "varable of func",
			code: dedent(`
				var Foo = func() {
					fmt.Println("hi")
				}`),
			wantValue: &ValueSnippet{
				Identifiers: []string{"Foo"},
				IsVar:       true,
				IsBlock:     false,
			},
		},
		{
			name: "multi assign",
			code: dedent(`
				var A, b = 1, 2`),
			wantValue: &ValueSnippet{
				Identifiers: []string{"A", "b"},
				IsVar:       true,
				IsBlock:     false,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Write the test code to a temporary file
			testFile := filepath.Join(tempDir, "test.go")
			err := os.WriteFile(testFile, []byte("package foo\n\n"+tc.code), 0644)
			assert.NoError(t, err)

			// Create a module for the test
			module := &Module{
				Name:         "testmodule",
				AbsolutePath: tempDir,
				Packages:     make(map[string]*Package),
			}

			// Create a package with the test file
			pkg, err := NewPackage("", tempDir, []string{"test.go"}, module)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, pkg)

			// Verify we got the expected value
			assert.Len(t, pkg.ValueSnippets, tc.wantCountDiffFromOne+1)
			if len(pkg.ValueSnippets) > 0 && tc.wantValue != nil {
				gotValue := pkg.ValueSnippets[0]
				assert.Equal(t, tc.wantValue.Identifiers, gotValue.Identifiers)
				assert.Equal(t, tc.wantValue.IsVar, gotValue.IsVar)
				assert.Equal(t, tc.wantValue.IsBlock, gotValue.IsBlock)
				assert.Equal(t, tc.wantValue.BlockDoc, gotValue.BlockDoc)
				if tc.wantValue.IdentifierDocs != nil {
					assert.Equal(t, tc.wantValue.IdentifierDocs, gotValue.IdentifierDocs)
				}
				assert.Equal(t, "test.go", gotValue.FileName)
			}

		})
	}
}

func TestValidateAndDetectTestPackage(t *testing.T) {
	gf := func(name, pkg string, isTest bool) *File {
		return &File{
			FileName:    name,
			PackageName: pkg,
			IsTest:      isTest,
		}
	}

	tests := []struct {
		desc        string
		files       map[string]*File
		wantBool    bool
		wantMainPkg string
		wantErr     bool
	}{
		{
			desc: "normal code only, no tests",
			files: map[string]*File{
				"foo.go": gf("foo.go", "foo", false),
			},
			wantBool:    false,
			wantMainPkg: "foo",
		},
		{
			desc: "white-box tests only (test-only package)",
			files: map[string]*File{
				"foo_test.go": gf("foo_test.go", "foo", true),
			},
			wantBool:    false,
			wantMainPkg: "foo",
		},
		{
			desc: "white-box + black-box tests (test-only directory)",
			files: map[string]*File{
				"foo_test.go":       gf("foo_test.go", "foo", true),
				"foo_black_test.go": gf("foo_black_test.go", "foo_test", true),
			},
			wantBool:    true,
			wantMainPkg: "foo",
		},
		{
			desc: "code + white-box tests",
			files: map[string]*File{
				"foo.go":      gf("foo.go", "foo", false),
				"foo_test.go": gf("foo_test.go", "foo", true),
			},
			wantBool:    false,
			wantMainPkg: "foo",
		},
		{
			desc: "code + white-box + black-box tests",
			files: map[string]*File{
				"foo.go":          gf("foo.go", "foo", false),
				"foo_test.go":     gf("foo_test.go", "foo", true),
				"foo_ext_test.go": gf("foo_ext_test.go", "foo_test", true),
			},
			wantBool:    true,
			wantMainPkg: "foo",
		},
		{
			desc: "mixed non-test packages → error",
			files: map[string]*File{
				"foo.go": gf("foo.go", "foo", false),
				"bar.go": gf("bar.go", "bar", false),
			},
			wantErr: true,
		},
		{
			desc: "test file with unrelated package → error",
			files: map[string]*File{
				"foo.go":      gf("foo.go", "foo", false),
				"bad_test.go": gf("bad_test.go", "bar_test", true),
			},
			wantErr: true,
		},
		{
			desc: "black-box only with no base package → fine (the normal packages can be suffixed with 'test')",
			files: map[string]*File{
				"foo_test.go": gf("foo_test.go", "foo_test", true),
			},
			wantBool:    false,
			wantMainPkg: "foo_test",
		},
	}

	for _, tc := range tests {
		got, mainPkg, err := validateAndDetectTestPackage(tc.files)
		if tc.wantErr {
			assert.Error(t, err, tc.desc)
		} else {
			assert.NoError(t, err, tc.desc)
			assert.Equal(t, tc.wantBool, got, tc.desc)
			assert.Equal(t, tc.wantMainPkg, mainPkg, tc.desc)
		}
	}
}

func TestPackageDocumentation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gopackage-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	pkgDoc1 := "// Package foo is great.\n"
	pkgDoc2 := "// Package foo is awesome.\n"

	tests := []struct {
		name           string
		pkgName        string
		files          map[string]string
		wantPackageDoc string            // content of documentation for identifier PackageIdentifier (ie, "package")
		wantFiles      map[string]string // map of filename -> excepted doc for that filename (using PackageIdentifierPerFile())
	}{
		{
			name:    "no docs",
			pkgName: "foo",
			files: map[string]string{
				"a.go": "package foo",
			},
		},
		{
			name:    "single doc",
			pkgName: "foo",
			files: map[string]string{
				"a.go": pkgDoc1 + "package foo",
			},
			wantPackageDoc: pkgDoc1,
			wantFiles: map[string]string{
				"a.go": pkgDoc1,
			},
		},
		{
			name:    "two docs",
			pkgName: "foo",
			files: map[string]string{
				"a.go": pkgDoc1 + "package foo",
				"b.go": pkgDoc2 + "package foo",
			},
			wantPackageDoc: pkgDoc1, // a.go is lexographically first
			wantFiles: map[string]string{
				"a.go": pkgDoc1,
				"b.go": pkgDoc2,
			},
		},
		{
			name:    "doc in pkgname.go",
			pkgName: "foo",
			files: map[string]string{
				"b.go":    pkgDoc1 + "package foo",
				"foo.go":  pkgDoc2 + "package foo",
				"main.go": "package foo",
			},
			wantPackageDoc: pkgDoc2, // foo.go is preferred
			wantFiles: map[string]string{
				"b.go":   pkgDoc1,
				"foo.go": pkgDoc2,
			},
		},
		{
			name:    "doc in doc.go",
			pkgName: "foo",
			files: map[string]string{
				"b.go":    pkgDoc1 + "package foo",
				"doc.go":  pkgDoc2 + "package foo",
				"main.go": "package foo",
			},
			wantPackageDoc: pkgDoc2, // doc.go is preferred
			wantFiles: map[string]string{
				"b.go":   pkgDoc1,
				"doc.go": pkgDoc2,
			},
		},
		{
			name:    "doc in doc.go and pkgname.go",
			pkgName: "foo",
			files: map[string]string{
				"b.go":    pkgDoc1 + "package foo",
				"doc.go":  pkgDoc2 + "package foo",
				"foo.go":  pkgDoc1 + "package foo",
				"main.go": "package foo",
			},
			wantPackageDoc: pkgDoc2, // doc.go is preferred over foo.go
			wantFiles: map[string]string{
				"b.go":   pkgDoc1,
				"doc.go": pkgDoc2,
				"foo.go": pkgDoc1,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create the test files
			var fileNames []string
			for fileName, content := range tc.files {
				filePath := filepath.Join(tempDir, fileName)
				err := os.WriteFile(filePath, []byte(content), 0644)
				assert.NoError(t, err)
				fileNames = append(fileNames, fileName)
			}

			// Create a module for the test
			module := &Module{
				Name:         "testmodule",
				AbsolutePath: tempDir,
				Packages:     make(map[string]*Package),
			}

			// Create and parse the package
			pkg, err := NewPackage("", tempDir, fileNames, module)
			assert.NoError(t, err)
			assert.NotNil(t, pkg)

			// Check the package-level snippet
			pkgSnippet := pkg.GetSnippet(PackageIdentifier)
			if tc.wantPackageDoc == "" {
				assert.Nil(t, pkgSnippet)
			} else {
				if assert.NotNil(t, pkgSnippet) {
					pds, ok := pkgSnippet.(*PackageDocSnippet)
					if assert.True(t, ok, "snippet is not a PackageDocSnippet") {
						assert.Equal(t, tc.wantPackageDoc, pds.Doc)
					}
				}
			}

			// Check the file-specific package snippets
			var foundFiles []string
			for _, s := range pkg.PackageDocSnippets {
				fileName := s.FileName
				wantDoc, ok := tc.wantFiles[fileName]
				assert.True(t, ok, "found unexpected package doc for file %s", fileName)
				assert.Equal(t, wantDoc, s.Doc, "doc for %s did not match", fileName)
				foundFiles = append(foundFiles, fileName)

				// Also check GetSnippet for the file-specific identifier
				fileSnippet := pkg.GetSnippet(PackageIdentifierPerFile(fileName))
				if assert.NotNil(t, fileSnippet, "snippet not found for %s", PackageIdentifierPerFile(fileName)) {
					pds, ok := fileSnippet.(*PackageDocSnippet)
					if assert.True(t, ok, "snippet is not a PackageDocSnippet") {
						assert.Equal(t, wantDoc, pds.Doc)
					}
				}
			}

			assert.Len(t, foundFiles, len(tc.wantFiles), "number of found package docs does not match expected")

			// Cleanup files for next test run
			for _, fileName := range fileNames {
				os.Remove(filepath.Join(tempDir, fileName))
			}
		})
	}
}

func TestFilterIdentifiers(t *testing.T) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "gocode-filteridentifiers-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Files:
	// - a.go: normal code file with various declarations including ambiguous ones
	// - b_test.go: white-box test file with a helper and a Test function
	// - gen.go: generated file with declarations
	aGo := dedent(`
		// Package foo docs
		package foo

		type TypeA struct{ X int }

		const ConstA = 1

		var VarA = 2 // var a documented

		var _ int

		func init() {}

		func FuncA() {}
	`)
	bTestGo := dedent(`
		package foo

		func helper() {}

		func TestSample(t *testing.T) {}
	`)
	genGo := dedent(`
		// Code generated by tool; DO NOT EDIT.
		package foo

		func GeneratedThing() {}

		const GeneratedConst = 3
	`)

	// Write files
	assert.NoError(t, os.WriteFile(filepath.Join(tempDir, "a.go"), []byte(aGo), 0644))
	assert.NoError(t, os.WriteFile(filepath.Join(tempDir, "b_test.go"), []byte(bTestGo), 0644))
	assert.NoError(t, os.WriteFile(filepath.Join(tempDir, "gen.go"), []byte(genGo), 0644))

	// Create a module for the test
	module := &Module{
		Name:         "testmodule",
		AbsolutePath: tempDir,
		Packages:     make(map[string]*Package),
	}

	// Create a package with the files
	pkg, err := NewPackage("", tempDir, []string{"a.go", "b_test.go", "gen.go"}, module)
	assert.NoError(t, err)
	assert.NotNil(t, pkg)

	// Collect ambiguous ids to use in targeted checks
	var ambiguousIDs []string
	for _, s := range pkg.Snippets() {
		for _, id := range s.IDs() {
			if IsAmbiguousIdentifier(id) {
				ambiguousIDs = append(ambiguousIDs, id)
			}
		}
	}
	// Sanity: we expect both an init() and an anonymous identifier
	assert.GreaterOrEqual(t, len(ambiguousIDs), 1)

	t.Run("default behavior", func(t *testing.T) {
		ids := pkg.FilterIdentifiers(nil, FilterIdentifiersOptions{})
		// Should include normal items and helper in test file; exclude test func, generated, ambiguous
		assert.Contains(t, ids, "FuncA")
		assert.Contains(t, ids, "TypeA")
		assert.Contains(t, ids, "ConstA")
		assert.Contains(t, ids, "VarA")
		assert.Contains(t, ids, PackageIdentifierPerFile("a.go"))
		assert.Contains(t, ids, "helper")

		assert.NotContains(t, ids, "TestSample")
		assert.NotContains(t, ids, "GeneratedThing")
		assert.NotContains(t, ids, "GeneratedConst")
		for _, amb := range ambiguousIDs {
			assert.NotContains(t, ids, amb)
		}
	})

	t.Run("no tests excludes test-file identifiers", func(t *testing.T) {
		ids := pkg.FilterIdentifiers(nil, FilterIdentifiersOptions{NoTests: true})
		assert.Contains(t, ids, "FuncA")
		assert.NotContains(t, ids, "helper")
	})

	t.Run("include test funcs includes TestSample", func(t *testing.T) {
		ids := pkg.FilterIdentifiers(nil, FilterIdentifiersOptions{IncludeTestFuncs: true})
		assert.Contains(t, ids, "TestSample")
		// still excludes generated by default
		assert.NotContains(t, ids, "GeneratedThing")
	})

	t.Run("include generated file identifiers", func(t *testing.T) {
		ids := pkg.FilterIdentifiers(nil, FilterIdentifiersOptions{IncludeGeneratedFile: true})
		assert.Contains(t, ids, "GeneratedThing")
		assert.Contains(t, ids, "GeneratedConst")
	})

	t.Run("include ambiguous identifiers only when requested", func(t *testing.T) {
		// When filtering a provided list, default should drop ambiguous
		ids := pkg.FilterIdentifiers(ambiguousIDs, FilterIdentifiersOptions{})
		assert.Len(t, ids, 0)

		// But including ambiguous should return them
		ids = pkg.FilterIdentifiers(ambiguousIDs, FilterIdentifiersOptions{IncludeAmbiguous: true})
		assert.ElementsMatch(t, ambiguousIDs, ids)
	})

	t.Run("kind filters: funcs only", func(t *testing.T) {
		ids := pkg.FilterIdentifiers(nil, FilterIdentifiersOptions{IncludeSnippetFuncs: true})
		assert.Contains(t, ids, "FuncA")
		assert.Contains(t, ids, "helper")
		assert.NotContains(t, ids, "TypeA")
		assert.NotContains(t, ids, "ConstA")
		assert.NotContains(t, ids, "VarA")
		// test funcs excluded by default
		assert.NotContains(t, ids, "TestSample")
	})

	t.Run("kind filters: consts only", func(t *testing.T) {
		ids := pkg.FilterIdentifiers(nil, FilterIdentifiersOptions{IncludeSnippetConst: true})
		assert.Contains(t, ids, "ConstA")
		assert.NotContains(t, ids, "VarA")
		assert.NotContains(t, ids, "FuncA")
		assert.NotContains(t, ids, "TypeA")
	})

	t.Run("kind filters: vars only", func(t *testing.T) {
		ids := pkg.FilterIdentifiers(nil, FilterIdentifiersOptions{IncludeSnippetVar: true})
		assert.Contains(t, ids, "VarA")
		assert.NotContains(t, ids, "ConstA")
	})

	t.Run("files allowlist restricts results", func(t *testing.T) {
		ids := pkg.FilterIdentifiers(nil, FilterIdentifiersOptions{Files: []string{"a.go"}})
		assert.Contains(t, ids, "FuncA")
		assert.Contains(t, ids, "TypeA")
		assert.NotContains(t, ids, "helper")
		assert.NotContains(t, ids, "GeneratedThing")
	})

	t.Run("filter provided identifiers only", func(t *testing.T) {
		subset := []string{"VarA", "FuncA", "NonExistent", "GeneratedThing"}
		ids := pkg.FilterIdentifiers(subset, FilterIdentifiersOptions{})
		// GeneratedThing excluded by default; NonExistent dropped
		assert.ElementsMatch(t, []string{"VarA", "FuncA"}, ids)

		// Including generated should allow it
		ids = pkg.FilterIdentifiers(subset, FilterIdentifiersOptions{IncludeGeneratedFile: true})
		assert.ElementsMatch(t, []string{"VarA", "FuncA", "GeneratedThing"}, ids)
	})

	// Verify OnlyAnyDocs restricts results to identifiers that have documentation
	t.Run("only any docs includes only documented identifiers", func(t *testing.T) {
		ids := pkg.FilterIdentifiers(nil, FilterIdentifiersOptions{OnlyAnyDocs: true})

		assert.Contains(t, ids, PackageIdentifierPerFile("a.go"))
		assert.Contains(t, ids, "VarA")

		// Undocumented identifiers should be excluded
		assert.NotContains(t, ids, "FuncA")
		assert.NotContains(t, ids, "TypeA")
		assert.NotContains(t, ids, "ConstA")
		assert.NotContains(t, ids, "helper")
		assert.NotContains(t, ids, "TestSample")
		assert.NotContains(t, ids, "GeneratedThing")
		assert.NotContains(t, ids, "GeneratedConst")
	})
}
