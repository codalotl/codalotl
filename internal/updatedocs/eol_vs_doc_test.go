package updatedocs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestEnforceEOLVsDocInAST_Basics(t *testing.T) {
	assert := assert.New(t)

	// Source snippet with a mix of doc and EOL comments.
	src := dedent(`
		package pkg

		var (
			// foo variable
			foo int
			bar string // bar variable

			//
			// divider:
			//

			// Very long comment 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890
			baz int
			qux int // qux
			fizz string // fizz Very long comment 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890
		)
	`)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	assert.NoError(err, "ParseFile should succeed")

	enforceEOLVsDocInAST(file, fset, Options{})

	type eolVsDocVsNone int
	const (
		tEOL eolVsDocVsNone = iota
		tDoc
		tNone
	)
	expectations := map[string]eolVsDocVsNone{
		"foo":  tEOL,
		"bar":  tEOL,
		"baz":  tDoc,
		"qux":  tDoc,
		"fizz": tDoc,
	}

	// Walk through the declarations and assert that each variable matches the
	// expected comment placement strategy (EOL comment vs doc comment vs none).
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, n := range vs.Names {
				exp, ok := expectations[n.Name]
				if !ok {
					continue // identifier not under test
				}

				switch exp {
				case tEOL:
					assert.Nil(vs.Doc, "%s should not have a doc comment", n.Name)
					assert.NotNil(vs.Comment, "%s should have an EOL comment", n.Name)
				case tDoc:
					assert.NotNil(vs.Doc, "%s should have a doc comment", n.Name)
					assert.Nil(vs.Comment, "%s should not have an EOL comment", n.Name)
				case tNone:
					assert.Nil(vs.Doc, "%s should not have a doc comment", n.Name)
					assert.Nil(vs.Comment, "%s should not have an EOL comment", n.Name)
				}

				// Make sure comments aren't reflowed:
				switch n.Name {
				case "baz":
					assert.Equal("Very long comment 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890\n", vs.Doc.Text())
				case "fizz":
					assert.Equal("fizz Very long comment 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890\n", vs.Doc.Text())
				}
			}
		}
	}
}

func TestEnforceEOLVsDocInAST_BasicStruct(t *testing.T) {
	assert := assert.New(t)

	// Source snippet with a mix of doc and EOL comments in a struct.
	src := dedent(`
		package pkg

		type MyStruct struct {
			// foo field
			foo int
			bar string // bar field

			//
			// divider:
			//

			// Very long comment 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890
			baz int
			qux int // qux
			fizz string // fizz Very long comment 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890
		}
	`)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	assert.NoError(err, "ParseFile should succeed")

	enforceEOLVsDocInAST(file, fset, Options{})

	type eolVsDocVsNone int
	const (
		tEOL eolVsDocVsNone = iota
		tDoc
		tNone
	)
	expectations := map[string]eolVsDocVsNone{
		"foo":  tEOL,
		"bar":  tEOL,
		"baz":  tDoc,
		"qux":  tDoc,
		"fizz": tDoc,
	}

	// Walk through the declarations and assert that each variable matches the
	// expected comment placement strategy (EOL comment vs doc comment vs none).
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, field := range st.Fields.List {
				for _, n := range field.Names {
					exp, ok := expectations[n.Name]
					if !ok {
						continue // identifier not under test
					}

					switch exp {
					case tEOL:
						assert.Nil(field.Doc, "%s should not have a doc comment", n.Name)
						assert.NotNil(field.Comment, "%s should have an EOL comment", n.Name)
					case tDoc:
						assert.NotNil(field.Doc, "%s should have a doc comment", n.Name)
						assert.Nil(field.Comment, "%s should not have an EOL comment", n.Name)
					case tNone:
						assert.Nil(field.Doc, "%s should not have a doc comment", n.Name)
						assert.Nil(field.Comment, "%s should not have an EOL comment", n.Name)
					}

					// Make sure comments aren't reflowed:
					switch n.Name {
					case "baz":
						assert.Equal("Very long comment 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890\n", field.Doc.Text())
					case "fizz":
						assert.Equal("fizz Very long comment 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890\n", field.Doc.Text())
					}
				}
			}
		}
	}
}

func TestDecideEOLOrDocForGroup(t *testing.T) {
	type args struct {
		fields      []*eolVsDocField
		softMaxCols int
	}
	tests := []struct {
		name     string
		args     args
		expected []bool // expected shouldBeEOL for each field in order
	}{
		{
			name: "three eligible fit -> all EOL",
			args: args{
				fields: []*eolVsDocField{
					{minCodeLength: 5, commentLength: 10},
					{minCodeLength: 5, commentLength: 10},
					{minCodeLength: 5, commentLength: 10},
				},
				softMaxCols: 80,
			},
			expected: []bool{true, true, true},
		},
		{
			name: "two eligible fit -> both EOL",
			args: args{
				fields: []*eolVsDocField{
					{minCodeLength: 5, commentLength: 10},
					{minCodeLength: 5, commentLength: 10},
				},
				softMaxCols: 80,
			},
			expected: []bool{true, true},
		},
		{
			name: "three eligible but exceed width -> none EOL",
			args: args{
				fields: []*eolVsDocField{
					{minCodeLength: 70, commentLength: 10},
					{minCodeLength: 70, commentLength: 10},
					{minCodeLength: 70, commentLength: 10},
				},
				softMaxCols: 80,
			},
			expected: []bool{false, false, false},
		},
		{
			name: "eligible + ineligible mix",
			args: args{
				fields: []*eolVsDocField{
					{minCodeLength: 5, commentLength: 10},                    // eligible
					{minCodeLength: 5, commentLength: 10, isMultiline: true}, // ineligible because multiline comment
					{minCodeLength: 5, commentLength: 10},                    // eligible but not enough run (only 1)
					{minCodeLength: 5, commentLength: 10},                    // eligible but not enough run (only 2)
					{minCodeLength: 5, commentLength: 10},                    // eligible (makes 3 consecutive)
				},
				softMaxCols: 80,
			},
			expected: []bool{false, false, true, true, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decideEOLVsDocForGroup(tt.args.fields, tt.args.softMaxCols)
			var got []bool
			for _, f := range tt.args.fields {
				got = append(got, f.shouldBeEOL)
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestGetEolVsDocFieldsForDeclBlock(t *testing.T) {
	type fieldExpectation struct {
		hasComment       bool // whether we expect any comment for the spec
		multilineComment bool
		multilineCode    bool
		minCodeLength    int
	}

	type testCase struct {
		name         string
		src          string
		wantGroups   [][]string                  // quick grouping assert
		expectations map[string]fieldExpectation // keyed by identifierKey
	}

	tests := []testCase{
		{
			name: "single group no floating comments",
			src: dedent(`
				var (
					a int // comment for a
					b string // comment for b
				)
			`),
			wantGroups: [][]string{{"a", "b"}},
			expectations: map[string]fieldExpectation{
				"a": {hasComment: true},
				"b": {hasComment: true},
			},
		},
		{
			name: "single group but newlines",
			src: dedent(`
				var (
					a int // comment for a


					b string // comment for b
				)
			`),
			wantGroups: [][]string{{"a", "b"}},
			expectations: map[string]fieldExpectation{
				"a": {hasComment: true},
				"b": {hasComment: true},
			},
		},
		{
			name: "single spec",
			src: dedent(`
				var (
					a int
				)
			`),
			wantGroups:   [][]string{{"a"}},
			expectations: map[string]fieldExpectation{"a": {}},
		},
		{
			name: "floating comment splits groups",
			src: dedent(`
				var (
					a int // comment for a

					// separator comment - should be floating

					b int
					c int
				)
			`),
			wantGroups: [][]string{{"a"}, {"b", "c"}},
			expectations: map[string]fieldExpectation{
				"a": {hasComment: true, minCodeLength: 5},
				"b": {hasComment: false},
				"c": {hasComment: false},
			},
		},
		{
			name: "multi-name spec and floating comment",
			src: dedent(`
				var (
					x, y int

					// mid-block comment

					z int
				)
			`),
			wantGroups: [][]string{{"x&y"}, {"z"}},
			expectations: map[string]fieldExpectation{
				"x&y": {hasComment: false, minCodeLength: 8},
				"z":   {hasComment: false, minCodeLength: 5},
			},
		},
		{
			name: "multline code",
			src: dedent(`
				var (
					// nested ...
					nested struct {
						a int
					}
				)
			`),
			wantGroups: [][]string{{"nested"}},
			expectations: map[string]fieldExpectation{
				"nested": {hasComment: true, multilineCode: true},
			},
		},
		{
			name: "multiline comment",
			src: dedent(`
				var (
					// 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789
					x = func(x int) {
						fmt.Println(x)
					}
				)
			`),
			wantGroups: [][]string{{"x"}},
			expectations: map[string]fieldExpectation{
				"x": {hasComment: true, multilineCode: true, multilineComment: true},
			},
		},
		{
			name: "codeLen when code comes pre-aligned",
			src: dedent(`
				var (
					x        int    // x
					otherVar string // otherVar
				)
			`),
			wantGroups: [][]string{{"x", "otherVar"}},
			expectations: map[string]fieldExpectation{
				"x":        {hasComment: true, minCodeLength: 5}, // just 5, not 12
				"otherVar": {hasComment: true, minCodeLength: 15},
			},
		},
		{
			name: "const with iota",
			src: dedent(`
				const (
					ntVar1 int = iota
					ntVar2
					ntVar3 // 3
				)
			`),
			wantGroups: [][]string{{"ntVar1", "ntVar2", "ntVar3"}},
			expectations: map[string]fieldExpectation{
				"ntVar1": {minCodeLength: 17},
				"ntVar2": {minCodeLength: 6},
				"ntVar3": {minCodeLength: 6, hasComment: true},
			},
		},
		{
			name: "type block",
			src: dedent(`
				type (
					foo int
					bar string
					baz struct {
						x int
					}
				)
			`),
			wantGroups: [][]string{{"foo", "bar", "baz"}},
			expectations: map[string]fieldExpectation{
				"foo": {minCodeLength: 7},
				"bar": {minCodeLength: 10},
				"baz": {multilineCode: true},
			},
		},
	}

	const tabWidth, softMaxCols = 4, 80

	for _, tc := range tests {
		tc := tc // capture
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			fileSrc := "package pkg\n\n" + tc.src
			file, err := parser.ParseFile(fset, "test.go", fileSrc, parser.ParseComments)
			if err != nil {
				t.Fatalf("ParseFile error: %v", err)
			}

			// Locate first value decl.
			var genDecl *ast.GenDecl
			for _, decl := range file.Decls {
				if gen, ok := decl.(*ast.GenDecl); ok {
					genDecl = gen
					break
				}
			}
			if genDecl == nil {
				t.Fatalf("no value declaration found in source")
			}

			// Run function under test.
			groups := getEolVsDocFieldsForDeclBlock(genDecl, fset, file.Comments, tabWidth, softMaxCols)

			// Basic grouping sanity check
			var gotGroups [][]string
			for _, g := range groups {
				var ids []string
				for _, f := range g {
					ids = append(ids, f.identifierKey)
				}
				gotGroups = append(gotGroups, ids)
			}
			if !reflect.DeepEqual(gotGroups, tc.wantGroups) {
				t.Fatalf("identifier groups mismatch. expected %v, got %v", tc.wantGroups, gotGroups)
			}

			// Property assertions per field (without re-implementing production logic)
			for _, g := range groups {
				for _, f := range g {
					exp, ok := tc.expectations[f.identifierKey]
					if !ok {
						t.Fatalf("missing expectation for identifier %s", f.identifierKey)
					}

					// Comment presence / absence
					if exp.hasComment && f.reflowedDocComment == "" {
						t.Errorf("%s: expected a comment, got none", f.identifierKey)
					}
					if !exp.hasComment && f.reflowedDocComment != "" {
						t.Errorf("%s: expected no comment, got one", f.identifierKey)
					}

					// Comment length consistency (if comment exists)
					if f.reflowedDocComment != "" {
						trimmed := strings.TrimSpace(f.reflowedDocComment)
						gotLen := utf8.RuneCountInString(trimmed)
						if gotLen != f.commentLength {
							t.Errorf("%s: commentLength mismatch, expected %d, got %d", f.identifierKey, gotLen, f.commentLength)
						}
						if exp.multilineComment != f.isMultiline {
							t.Errorf("%s: isMultiline expected %v, got %v", f.identifierKey, exp.multilineComment, f.isMultiline)
						}
					} else {
						if f.commentLength != 0 {
							t.Errorf("%s: expected commentLength 0, got %d", f.identifierKey, f.commentLength)
						}
						if f.isMultiline {
							t.Errorf("%s: expected isMultiline false, got true", f.identifierKey)
						}
					}

					if exp.multilineCode != f.codeIsMultiline {
						t.Errorf("%s: expected codeIsMultiline = %v, got %v", f.identifierKey, exp.multilineCode, f.codeIsMultiline)
					}

					if f.codeIsMultiline {
						if f.minCodeLength != 0 {
							t.Errorf("%s: expect multiline code to be zero len, got %d", f.identifierKey, f.minCodeLength)
						}
					} else {
						if f.minCodeLength <= 0 {
							t.Errorf("%s: expected positive minCodeLength, got %d", f.identifierKey, f.minCodeLength)
						}
					}

					if exp.minCodeLength > 0 {
						if f.minCodeLength != exp.minCodeLength {
							t.Errorf("%s: minCodeLength differ (actual=%v vs expected=%v)", f.identifierKey, f.minCodeLength, exp.minCodeLength)
						}
					}

					// shouldBeEOL is always false immediately after grouping
					if f.shouldBeEOL {
						t.Errorf("%s: expected shouldBeEOL false right after grouping", f.identifierKey)
					}
				}
			}
		})
	}
}

func TestGroupDeclBlockNonBlock(t *testing.T) {
	const tabWidth, softMaxCols = 4, 80

	type testCase struct {
		name string
		src  string
	}

	tests := []testCase{
		{
			name: "single var spec",
			src: dedent(`
				var x int // comment for x
			`),
		},
		{
			name: "single const spec",
			src: dedent(`
				const y = 42
			`),
		},
		{
			name: "single type spec",
			src: dedent(`
				type Foo struct{}
			`),
		},
		{
			name: "empty var decl", // not exactly non-block, but we can test it here
			src: dedent(`
				var ()
			`),
		},
	}

	for _, tc := range tests {
		fset := token.NewFileSet()
		fileSrc := "package pkg\n\n" + tc.src
		file, err := parser.ParseFile(fset, "test.go", fileSrc, parser.ParseComments)
		if err != nil {
			t.Fatalf("ParseFile error: %v", err)
		}

		// Locate first GenDecl.
		var genDecl *ast.GenDecl
		for _, decl := range file.Decls {
			if gen, ok := decl.(*ast.GenDecl); ok {
				genDecl = gen
				break
			}
		}
		if genDecl == nil {
			t.Fatalf("no GenDecl found in source for %s", tc.name)
		}

		groups := getEolVsDocFieldsForDeclBlock(genDecl, fset, file.Comments, tabWidth, softMaxCols)
		if groups != nil {
			t.Fatalf("%s: expected nil groups for non-block declaration, got %v", tc.name, groups)
		}
	}
}

func TestGetEolVsDocFieldsForStructType(t *testing.T) {
	type fieldExpectation struct {
		hasComment       bool // whether we expect any comment for the spec
		multilineComment bool
		multilineCode    bool
		minCodeLength    int
	}

	type testCase struct {
		name         string
		src          string
		wantGroups   [][]string                  // quick grouping assert
		expectations map[string]fieldExpectation // keyed by identifierKey (e.g., "MyStruct.FieldName")
	}

	tests := []testCase{
		{
			name: "single group no floating comments",
			src: dedent(`
				type MyStruct struct {
					A int    // comment for a
					B string // comment for b
				}
			`),
			wantGroups: [][]string{{"MyStruct.A", "MyStruct.B"}},
			expectations: map[string]fieldExpectation{
				"MyStruct.A": {hasComment: true, minCodeLength: 5},
				"MyStruct.B": {hasComment: true, minCodeLength: 8},
			},
		},
		{
			name:       "field tag",
			src:        "type MyStruct struct {\n\tA int `bar:\"x\"`\n\tB int ``\n}\n",
			wantGroups: [][]string{{"MyStruct.A", "MyStruct.B"}},
			expectations: map[string]fieldExpectation{
				"MyStruct.A": {hasComment: false, minCodeLength: 15},
				"MyStruct.B": {hasComment: false, minCodeLength: 8},
			},
		},
		{
			name: "floating comment splits groups",
			src: dedent(`
				type MyStruct struct {
					A int // comment for a

					// separator

					B int
				}
			`),
			wantGroups: [][]string{{"MyStruct.A"}, {"MyStruct.B"}},
			expectations: map[string]fieldExpectation{
				"MyStruct.A": {hasComment: true, minCodeLength: 5},
				"MyStruct.B": {hasComment: false, minCodeLength: 5},
			},
		},
		{
			name: "multiline field",
			src: dedent(`
				type MyStruct struct {
					F1 struct {
						X int
					}
				}
			`),
			wantGroups: [][]string{{"MyStruct.F1.X"}, {"MyStruct.F1"}},
			expectations: map[string]fieldExpectation{
				"MyStruct.F1.X": {minCodeLength: 5},
				"MyStruct.F1":   {minCodeLength: 0, multilineCode: true},
			},
		},
		{
			name: "multiline comment",
			src: dedent(`
				type MyStruct struct {
					// 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789
					F1 int
				}
			`),
			wantGroups: [][]string{{"MyStruct.F1"}},
			expectations: map[string]fieldExpectation{
				"MyStruct.F1": {hasComment: true, multilineComment: true, minCodeLength: 6},
			},
		},
		{
			name: "anonymous fields",
			src: dedent(`
				type MyStruct struct {
					string // anon
					http.Request
				}
			`),
			wantGroups: [][]string{{"MyStruct.string", "MyStruct.http.Request"}},
			expectations: map[string]fieldExpectation{
				"MyStruct.string":       {hasComment: true, minCodeLength: 6},
				"MyStruct.http.Request": {hasComment: false, minCodeLength: 12},
			},
		},
		{
			name: "nested structs",
			src: dedent(`
				type MyStruct struct {

					// s1
					s1 string

					foo struct {
						bar int // bar

						// divide

						baz int // baz
					}

					s2 string // s2

					// qux comment:
					qux *struct {
						// comment

						q1 int
					}
				}
			`),
			wantGroups: [][]string{{"MyStruct.foo.bar"}, {"MyStruct.foo.baz"}, {"MyStruct.qux.q1"}, {"MyStruct.s1", "MyStruct.foo", "MyStruct.s2", "MyStruct.qux"}},
			expectations: map[string]fieldExpectation{
				"MyStruct.s1":      {hasComment: true, minCodeLength: 9},
				"MyStruct.foo":     {hasComment: false, multilineCode: true},
				"MyStruct.foo.bar": {hasComment: true, minCodeLength: 7},
				"MyStruct.foo.baz": {hasComment: true, minCodeLength: 7},
				"MyStruct.s2":      {hasComment: true, minCodeLength: 9},
				"MyStruct.qux":     {hasComment: true, multilineCode: true},
				"MyStruct.qux.q1":  {hasComment: false, minCodeLength: 6},
			},
		},
		{
			name: "simple generic struct",
			src: dedent(`
				type MyStruct[T any] struct {
					A T    // comment for A
					B string // comment for B
				}
			`),
			wantGroups: [][]string{{"MyStruct.A", "MyStruct.B"}},
			expectations: map[string]fieldExpectation{
				"MyStruct.A": {hasComment: true, minCodeLength: 3},
				"MyStruct.B": {hasComment: true, minCodeLength: 8},
			},
		},
		{
			name: "generic with constraints",
			src: dedent(`
				type MyStruct[T comparable, V any] struct {
					Data    T // data field
					Pointer *V
				}
			`),
			wantGroups: [][]string{{"MyStruct.Data", "MyStruct.Pointer"}},
			expectations: map[string]fieldExpectation{
				"MyStruct.Data":    {hasComment: true, minCodeLength: 6},
				"MyStruct.Pointer": {hasComment: false, minCodeLength: 10},
			},
		},
		{
			name: "embedded generic type",
			src: dedent(`
				type MyStruct[T any] struct {
					Other[T] // embedded generic
					Field    T
				}
			`),
			wantGroups: [][]string{{"MyStruct.Other", "MyStruct.Field"}},
			expectations: map[string]fieldExpectation{
				"MyStruct.Other": {hasComment: true, minCodeLength: 8},
				"MyStruct.Field": {hasComment: false, minCodeLength: 7},
			},
		},
		{
			name: "field with generic func type",
			src: dedent(`
				type MyStruct[T any] struct {
					MyFunc func(T) (T, error) // generic func type
				}
			`),
			wantGroups: [][]string{{"MyStruct.MyFunc"}},
			expectations: map[string]fieldExpectation{
				"MyStruct.MyFunc": {hasComment: true, minCodeLength: 25},
			},
		},
	}

	const tabWidth, softMaxCols = 4, 80

	for _, tc := range tests {
		tc := tc // capture
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			// For anonymous fields test we need to import http
			fileSrc := "package pkg\n\nimport \"net/http\"\n\n" + tc.src
			file, err := parser.ParseFile(fset, "test.go", fileSrc, parser.ParseComments)
			if err != nil {
				t.Fatalf("ParseFile error: %v", err)
			}

			// Locate struct type.
			var structType *ast.StructType
			var typeSpec *ast.TypeSpec
			ast.Inspect(file, func(n ast.Node) bool {
				if ts, ok := n.(*ast.TypeSpec); ok {
					if st, ok := ts.Type.(*ast.StructType); ok {
						structType = st
						typeSpec = ts
						return false
					}
				}
				return true
			})

			if structType == nil {
				t.Fatalf("no struct type found in source")
			}

			// Run function under test.
			groups := getEolVsDocFieldsForStructType(typeSpec.Name.Name, structType, fset, file.Comments, 0, tabWidth, softMaxCols)

			// Basic grouping sanity check
			var gotGroups [][]string
			for _, g := range groups {
				var ids []string
				for _, f := range g {
					ids = append(ids, f.identifierKey)
				}
				gotGroups = append(gotGroups, ids)
			}

			sort.Slice(gotGroups, func(i, j int) bool {
				return gotGroups[i][0] < gotGroups[j][0]
			})
			sort.Slice(tc.wantGroups, func(i, j int) bool {
				return tc.wantGroups[i][0] < tc.wantGroups[j][0]
			})

			if !reflect.DeepEqual(gotGroups, tc.wantGroups) {
				t.Fatalf("identifier groups mismatch. expected %v, got %v", tc.wantGroups, gotGroups)
			}

			// Property assertions per field
			for _, g := range groups {
				for _, f := range g {
					exp, ok := tc.expectations[f.identifierKey]
					if !ok {
						t.Fatalf("missing expectation for identifier %s", f.identifierKey)
					}

					// Comment presence / absence
					if exp.hasComment && f.reflowedDocComment == "" {
						t.Errorf("%s: expected a comment, got none", f.identifierKey)
					}
					if !exp.hasComment && f.reflowedDocComment != "" {
						t.Errorf("%s: expected no comment, got one", f.identifierKey)
					}

					// Comment length consistency (if comment exists)
					if f.reflowedDocComment != "" {
						trimmed := strings.TrimSpace(f.reflowedDocComment)
						gotLen := utf8.RuneCountInString(trimmed)
						if gotLen != f.commentLength {
							t.Errorf("%s: commentLength mismatch, expected %d, got %d", f.identifierKey, gotLen, f.commentLength)
						}
						if exp.multilineComment != f.isMultiline {
							t.Errorf("%s: isMultiline expected %v, got %v", f.identifierKey, exp.multilineComment, f.isMultiline)
						}
					} else {
						if f.commentLength != 0 {
							t.Errorf("%s: expected commentLength 0, got %d", f.identifierKey, f.commentLength)
						}
						if f.isMultiline {
							t.Errorf("%s: expected isMultiline false, got true", f.identifierKey)
						}
					}

					// Code metrics
					if !exp.multilineCode && f.minCodeLength <= 0 {
						t.Errorf("%s: expected positive minCodeLength, got %d", f.identifierKey, f.minCodeLength)
					}
					if exp.minCodeLength > 0 {
						if f.minCodeLength != exp.minCodeLength {
							t.Errorf("%s: minCodeLength differ (actual=%v vs expected=%v)", f.identifierKey, f.minCodeLength, exp.minCodeLength)
						}
					}
					if f.identifierKey == "MyStruct.F1.X" {
						// The parent field F1 is multiline, but the function under test recurses and gives us the field X.
						// We can't easily test the multiline aspect of F1 here.
					} else if exp.multilineCode != f.codeIsMultiline {
						t.Errorf("%s: expected codeIsMultiine = %v, got %v", f.identifierKey, exp.multilineCode, f.codeIsMultiline)
					}

					// shouldBeEOL is always false immediately after grouping
					if f.shouldBeEOL {
						t.Errorf("%s: expected shouldBeEOL false right after grouping", f.identifierKey)
					}
				}
			}
		})
	}
}

func TestEnforceEOLVsDocInAST_InterfaceBasics(t *testing.T) {
	assert := assert.New(t)

	src := dedent(`
		package pkg

		type MyInterface interface {
			// method doc
			Foo(x int) error
			Bar() // bar eol

			// divider

			Baz() // baz eol
		}
	`)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	assert.NoError(err)

	enforceEOLVsDocInAST(file, fset, Options{})

	// Expect Foo keeps Doc; Bar/Baz EOL become Doc
	found := false
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			it, ok := ts.Type.(*ast.InterfaceType)
			if !ok {
				continue
			}
			found = true
			for _, m := range it.Methods.List {
				name := ""
				if len(m.Names) > 0 {
					name = m.Names[0].Name
				} else {
					// embedded interface; skip in this test
					continue
				}
				switch name {
				case "Foo":
					assert.NotNil(m.Doc)
					assert.Nil(m.Comment)
				case "Bar":
					assert.NotNil(m.Doc)
					assert.Nil(m.Comment)
				case "Baz":
					assert.NotNil(m.Doc)
					assert.Nil(m.Comment)
				}
			}
		}
	}
	assert.True(found, "interface not found")
}

func TestEnforceEOLVsDocInAST_InterfaceInTypeBlock(t *testing.T) {
	assert := assert.New(t)

	src := dedent(`
		package pkg

		type (
			// Some other type above
			S struct{}

			MyInterface interface {
				Embedded // embedded interface eol
				Qux()    // qux eol
			}
		)

		type Embedded interface{ }
	`)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	assert.NoError(err)

	enforceEOLVsDocInAST(file, fset, Options{})

	var iface *ast.InterfaceType
	ast.Inspect(file, func(n ast.Node) bool {
		if ts, ok := n.(*ast.TypeSpec); ok && ts.Name.Name == "MyInterface" {
			if it, ok := ts.Type.(*ast.InterfaceType); ok {
				iface = it
				return false
			}
		}
		return true
	})
	if iface == nil {
		t.Fatalf("interface not found")
	}

	// Validate Embedded and Qux are Doc comments, not EOL
	for _, m := range iface.Methods.List {
		if len(m.Names) == 0 {
			// embedded interface
			assert.NotNil(m.Doc)
			assert.Nil(m.Comment)
			continue
		}
		if m.Names[0].Name == "Qux" {
			assert.NotNil(m.Doc)
			assert.Nil(m.Comment)
		}
	}
}
