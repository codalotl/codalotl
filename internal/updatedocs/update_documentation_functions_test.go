package updatedocs

import "testing"

func TestUpdateDocumentationFunctionsTableDriven(t *testing.T) {
	tests := []tableDrivenDocUpdateTest{
		{
			name: "add function comment with body-less function",
			existingSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					func ExampleFunc() {}
				`),
			},
			snippets: []string{
				dedent(`
					// ExampleFunc demonstrates the example functionality.
					func ExampleFunc()
				`),
			},
			newSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					// ExampleFunc demonstrates the example functionality.
					func ExampleFunc() {}
				`),
			},
		},
		{
			name: "edits function comment with body-less function",
			existingSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					// ExampleFunc already had an existing comment
					// that says something.
					func ExampleFunc() {}
				`),
			},
			snippets: []string{
				dedent(`
					// ExampleFunc demonstrates the example functionality.
					// It has
					// three lines.
					func ExampleFunc()
				`),
			},
			newSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					// ExampleFunc demonstrates the example functionality.
					// It has
					// three lines.
					func ExampleFunc() {}
				`),
			},
		},
		{
			name: "add function comment - unexported func",
			existingSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					func exampleFunc() {}
				`),
			},
			snippets: []string{
				dedent(`
					// exampleFunc demonstrates the example functionality.
					func exampleFunc()
				`),
			},
			newSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					// exampleFunc demonstrates the example functionality.
					func exampleFunc() {}
				`),
			},
		},
		{
			name: "multiple function edits",
			existingSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					// ExampleFunc already had an existing comment
					// that says something.
					func ExampleFunc() {}

					func AnotherFunc() {}

					// ThirdFunc
					func ThirdFunc(a int) int {
						return a
					}

					func FourthFunc() {}
				`),
			},
			snippets: []string{
				dedent(`
					// ExampleFunc demonstrates the example functionality.
					func ExampleFunc()
				`),
				dedent(`
					// AnotherFunc is
					// a great function.
					func AnotherFunc()
				`),
				dedent(`
					// ThirdFunc is wonderful.
					func ThirdFunc(a int) int
				`),
			},
			newSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					// ExampleFunc demonstrates the example functionality.
					func ExampleFunc() {}

					// AnotherFunc is
					// a great function.
					func AnotherFunc() {}

					// ThirdFunc is wonderful.
					func ThirdFunc(a int) int {
						return a
					}

					func FourthFunc() {}
				`),
			},
		},
		{
			name: "edits function comment with missing closing brace",
			existingSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					// ExampleFunc already had an existing comment
					// that says something.
					func ExampleFunc() {
						return
					}
				`),
			},
			snippets: []string{
				dedent(`
					// ExampleFunc demonstrates the example functionality.
					func ExampleFunc() {
				`),
			},
			newSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					// ExampleFunc demonstrates the example functionality.
					func ExampleFunc() {
						return
					}
				`),
			},
		},
		{
			name: "function has eol comment which converts to doc comment",
			existingSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					func ExampleFunc(a int) {}
				`),
			},
			snippets: []string{
				dedent(`
					func ExampleFunc(a int) // ExampleFunc demonstrates the example functionality.
				`),
			},
			newSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					// ExampleFunc demonstrates the example functionality.
					func ExampleFunc(a int) {}
				`),
			},
		},
		{
			name: "function can be documented with a block comment",
			existingSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					func ExampleFunc(a int) {}
				`),
			},
			snippets: []string{
				dedent(`
					/* Example */
					func ExampleFunc(a int)
				`),
			},
			newSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					/* Example */
					func ExampleFunc(a int) {}
				`),
			},
		},
		{
			name: "function can be documented if there's an errant block comment somewhere",
			existingSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					func ExampleFunc(a int) /* errant */ {}
				`),
			},
			snippets: []string{
				dedent(`
					// Example
					func ExampleFunc(a int)
				`),
			},
			newSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					// Example
					func ExampleFunc(a int) /* errant */ {}
				`),
			},
		},
		{
			name: "function no comment",
			existingSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					func ExampleFunc(a int) {}
				`),
			},
			snippets: []string{
				dedent(`
					func ExampleFunc(a int)
				`),
			},
		},
		{
			name: "function snippet errors - both doc and eol comment",
			existingSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					func ExampleFunc(a int) {}
				`),
			},
			snippets: []string{
				dedent(`
					// ExampleFunc demonstrates the example functionality.
					func ExampleFunc(a int) // ExampleFunc demonstrates the example functionality2.
				`),
			},
			expectSnippetErrCount: 1,
			expectSnippetErrLike:  "func ExampleFunc has both doc comment and end-of-line comment",
		},
		{
			// NOTE: I don't love this, but LLMs can sometimes do weird things when sending back documentation:
			//   - they can rename parameters, or drop their name
			//   - they can name return values where they are unnamed in source
			//   - I guess i haven't seen them totally removing parameters, to be fair
			// The best user experience is just to permit loosey-goosey matching. We can easily change the matching behavior if we need to.
			name: "function matches on function name, ignoring arg mismatches",
			existingSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					func ExampleFunc(a int) {}

					func (t *T) ExampleFunc() {}
				`),
			},
			snippets: []string{
				dedent(`
					// ExampleFunc demonstrates the example functionality.
					func ExampleFunc()
				`),
			},
			newSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					// ExampleFunc demonstrates the example functionality.
					func ExampleFunc(a int) {}

					func (t *T) ExampleFunc() {}
				`),
			},
		},
		{
			// NOTE: I don't love that callers would call us with a body. But since LLMs sometimes do, we may as well just handle it
			// and sweep the fact that the body could be different under the rug.
			name: "function which has a body",
			existingSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					func ExampleFunc(a int) {
						fmt.Println(a)
					}
				`),
			},
			snippets: []string{
				dedent(`
					// ExampleFunc demonstrates the example functionality.
					func ExampleFunc(a int) {
						fmt.Println(a)
					}
				`),
			},
			newSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					// ExampleFunc demonstrates the example functionality.
					func ExampleFunc(a int) {
						fmt.Println(a)
					}
				`),
			},
		},
		{
			name: "function snippet errors - reject updates",
			existingSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					// Existing
					func ExampleFunc() {}
				`),
			},
			snippets: []string{
				dedent(`
					// ExampleFunc demonstrates the example functionality.
					func ExampleFunc()
				`),
			},
			options:               Options{RejectUpdates: true},
			expectSnippetErrCount: 1,
			expectPartial:         true,
		},
	}

	for _, testCase := range tests {
		runTableDrivenDocUpdateTest(t, testCase)
	}
}
