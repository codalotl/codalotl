package updatedocs

import "testing"

func TestUpdateDocumentationPackagesTableDriven(t *testing.T) {
	tests := []tableDrivenDocUpdateTest{
		{
			name: "add package comment - even if pkgname.go, make doc.go",
			existingSource: map[string]string{
				"mypkg.go": dedent(`
					package mypkg
				`),
				"other.go": dedent(`
					package mypkg
				`),
			},
			snippets: []string{
				dedent(`
					// Package comment
					package mypkg
				`),
			},
			newSource: map[string]string{
				"doc.go": dedent(`
					// Package comment
					package mypkg
				`),
			},
		},
		{
			name: "add package comment - if doc.go, use it",
			existingSource: map[string]string{
				"mypkg.go": dedent(`
					package mypkg
				`),
				"doc.go": dedent(`
					package mypkg
				`),
			},
			snippets: []string{
				dedent(`
					// Package comment
					package mypkg
				`),
			},
			newSource: map[string]string{
				"doc.go": dedent(`
					// Package comment
					package mypkg
				`),
			},
		},
		{
			name: "package comment - create new file",
			existingSource: map[string]string{
				"existing.go": dedent(`
					package mypkg
				`),
			},
			snippets: []string{
				dedent(`
					// Package-level comment
					// Explains things
					package mypkg
				`),
			},
			newSource: map[string]string{
				"doc.go": dedent(`
					// Package-level comment
					// Explains things
					package mypkg
				`),
			},
		},
		{
			name: "package comment - update docs",
			existingSource: map[string]string{
				"doc.go": dedent(`
					// Existing comment
					package mypkg

					// Version is version
					var Version = 1
				`),
				"mypkg.go": dedent(`
					// Other existing comment
					package mypkg
				`),
			},
			snippets: []string{
				dedent(`
					// New comment
					// New comment
					// New comment
					// New comment
					package mypkg
				`),
			},
			newSource: map[string]string{
				"doc.go": dedent(`
					// New comment
					// New comment
					// New comment
					// New comment
					package mypkg

					// Version is version
					var Version = 1
				`),
			},
		},
		{
			name: "existing comments - priority to doc",
			existingSource: map[string]string{
				"existing.go": dedent(`
					// existing
					package mypkg
				`),
				"mypkg.go": dedent(`
					// mypkg
					package mypkg
				`),
				"doc.go": dedent(`
					// doc
					package mypkg
				`),
			},
			snippets: []string{
				dedent(`
					// new
					package mypkg
				`),
			},
			newSource: map[string]string{
				"doc.go": dedent(`
					// new
					package mypkg
				`),
			},
		},
		{
			name: "existing comments - priority to pkgname",
			existingSource: map[string]string{
				"existing.go": dedent(`
					// existing
					package mypkg
				`),
				"mypkg.go": dedent(`
					// mypkg
					package mypkg
				`),
			},
			snippets: []string{
				dedent(`
					// new
					package mypkg
				`),
			},
			newSource: map[string]string{
				"mypkg.go": dedent(`
					// new
					package mypkg
				`),
			},
		},
		{
			name: "existing comments - priority lexicographical",
			existingSource: map[string]string{
				"a.go": dedent(`
					// a
					package mypkg
				`),
				"b.go": dedent(`
					// b
					package mypkg
				`),
			},
			snippets: []string{
				dedent(`
					// new
					package mypkg
				`),
			},
			newSource: map[string]string{
				"a.go": dedent(`
					// new
					package mypkg
				`),
			},
		},
		{
			name: "existing comments - priority the one with a comment",
			existingSource: map[string]string{
				"a.go": dedent(`
					package mypkg
				`),
				"b.go": dedent(`
					// b
					package mypkg
				`),
			},
			snippets: []string{
				dedent(`
					// new
					package mypkg
				`),
			},
			newSource: map[string]string{
				"b.go": dedent(`
					// new
					package mypkg
				`),
			},
		},
	}

	for _, testCase := range tests {
		runTableDrivenDocUpdateTest(t, testCase)
	}
}
