package updatedocs

import "testing"

func TestUpdateDocumentationGenerics(t *testing.T) {
	tests := []tableDrivenDocUpdateTest{
		{
			name: "generic struct type - add doc comment",
			existingSource: map[string]string{
				"vect.go": dedent(`
					package mypkg

					type Vector[T any] struct {
						X, Y T
					}
				`),
			},
			snippets: []string{
				dedent(`
					// Vector is a two-dimensional vector.
					type Vector[T any] struct {
						X, Y T // X and Y
					}
				`),
			},
			newSource: map[string]string{
				"vect.go": dedent(`
					package mypkg

					// Vector is a two-dimensional vector.
					type Vector[T any] struct {
						X, Y T // X and Y
					}
				`),
			},
		},
		{
			name: "generic function - add doc comment",
			existingSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					func Identity[T any](v T) T {
						return v
					}
				`),
			},
			snippets: []string{
				dedent(`
					// Identity returns its argument unchanged.
					func Identity[T any](v T) T
				`),
			},
			newSource: map[string]string{
				"funcs.go": dedent(`
					package mypkg

					// Identity returns its argument unchanged.
					func Identity[T any](v T) T {
						return v
					}
				`),
			},
		},
		{
			name: "nested generic struct type - add doc comment",
			existingSource: map[string]string{
				"node.go": dedent(`
					package mypkg

					type Node[T any] struct {
						Value    T
						Children []*Node[T]
					}
				`),
			},
			snippets: []string{
				dedent(`
					// Node represents a tree node.
					type Node[T any] struct {
						Value    T          // Value stored in the node
						Children []*Node[T] // Child nodes
					}
				`),
			},
			newSource: map[string]string{
				"node.go": dedent(`
					package mypkg

					// Node represents a tree node.
					type Node[T any] struct {
						Value    T          // Value stored in the node
						Children []*Node[T] // Child nodes
					}
				`),
			},
		},
		{
			name: "struct with embedded ptr to generic type",
			existingSource: map[string]string{
				"node.go": dedent(`
					package mypkg

					type Node struct {
						*streamable[OtherType]
					}
				`),
			},
			snippets: []string{
				dedent(`
					// Node represents...
					type Node struct {
						// stramable...
						*streamable[OtherType]
					}
				`),
			},
			newSource: map[string]string{
				"node.go": dedent(`
					package mypkg

					// Node represents...
					type Node struct {
						// stramable...
						*streamable[OtherType]
					}
				`),
			},
		},
		{
			name: "generics interfaces - documenting unions",
			existingSource: map[string]string{
				"node.go": dedent(`
					package mypkg

					type Num interface {
						~int
					}
				`),
			},
			snippets: []string{
				dedent(`
					// Num.
					type Num interface {
						~int // all ints
					}
				`),
			},
			newSource: map[string]string{
				"node.go": dedent(`
					package mypkg

					// Num.
					type Num interface {
						~int // all ints
					}
				`),
			},
		},
		{
			name: "generics interfaces - documenting unions AND other methods",
			existingSource: map[string]string{
				"node.go": dedent(`
					package mypkg

					type Num interface {
						~int
						String() string
					}
				`),
			},
			snippets: []string{
				dedent(`
					// Num.
					type Num interface {
						~int // all ints

						// stringer
						String() string
					}
				`),
			},
			newSource: map[string]string{
				"node.go": dedent(`
					package mypkg

					// Num.
					type Num interface {
						~int // all ints

						// stringer
						String() string
					}
				`),
			},
		},
		{
			name: "generic method - add doc comment",
			existingSource: map[string]string{
				"vect.go": dedent(`
					package mypkg

					type Vector[T any] struct {
						X, Y T
					}

					func (v *Vector[T]) Magnitude() T {
						return v.X // stub implementation
					}
				`),
			},
			snippets: []string{
				dedent(`
					// Magnitude returns the magnitude.
					func (v *Vector[T]) Magnitude() T
				`),
			},
			newSource: map[string]string{
				"vect.go": dedent(`
					package mypkg

					type Vector[T any] struct {
						X, Y T
					}

					// Magnitude returns the magnitude.
					func (v *Vector[T]) Magnitude() T {
						return v.X // stub implementation
					}
				`),
			},
		},
	}

	for _, tc := range tests {
		runTableDrivenDocUpdateTest(t, tc)
	}
}
