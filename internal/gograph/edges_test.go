package gograph

import (
	"testing"
)

func TestIdentifiersFromTo(t *testing.T) {
	// Create a test package with various dependencies
	source := `
type Foo struct {
	bar Bar
}

type Bar struct {
	value int
}

func (f *Foo) Method() {
	b := Bar{}
	f.bar = b
}

func usesFoo() {
	f := Foo{}
	f.Method()
}

var globalFoo Foo

const myConst = 42
`

	// Use the existing newTestPackage helper
	pkg := newTestPackage(t, source)

	// Create the graph
	graph, err := NewGoGraph(pkg)
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}

	// Test cases for IdentifiersFrom
	t.Run("IdentifiersFrom", func(t *testing.T) {
		tests := []struct {
			name     string
			fromID   string
			expected map[string]bool
		}{
			{
				name:   "Foo references Bar",
				fromID: "Foo",
				expected: map[string]bool{
					"Bar": true,
				},
			},
			{
				name:   "Method references Foo and Bar",
				fromID: "*Foo.Method",
				expected: map[string]bool{
					"Foo": true,
					"Bar": true,
				},
			},
			{
				name:   "usesFoo references Foo and Method",
				fromID: "usesFoo",
				expected: map[string]bool{
					"Foo":         true,
					"*Foo.Method": true,
				},
			},
			{
				name:   "globalFoo references Foo",
				fromID: "globalFoo",
				expected: map[string]bool{
					"Foo": true,
				},
			},
			{
				name:     "Bar has no outgoing edges",
				fromID:   "Bar",
				expected: map[string]bool{},
			},
			{
				name:     "myConst has no outgoing edges",
				fromID:   "myConst",
				expected: map[string]bool{},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := graph.IdentifiersFrom(tt.fromID)

				// Convert result to map for easier comparison
				resultMap := make(map[string]bool)
				for _, id := range result {
					resultMap[id] = true
				}

				// Check if all expected identifiers are present
				for expectedID := range tt.expected {
					if !resultMap[expectedID] {
						t.Errorf("Expected %s -> %s edge, but it was not found", tt.fromID, expectedID)
					}
				}

				// Check if there are no unexpected identifiers
				for id := range resultMap {
					if !tt.expected[id] {
						t.Errorf("Unexpected edge %s -> %s", tt.fromID, id)
					}
				}
			})
		}
	})

	// Test cases for IdentifiersTo
	t.Run("IdentifiersTo", func(t *testing.T) {
		tests := []struct {
			name     string
			toID     string
			expected map[string]bool
		}{
			{
				name: "Bar is referenced by Foo and Method",
				toID: "Bar",
				expected: map[string]bool{
					"Foo":         true,
					"*Foo.Method": true,
				},
			},
			{
				name: "Foo is referenced by Method, usesFoo, and globalFoo",
				toID: "Foo",
				expected: map[string]bool{
					"*Foo.Method": true,
					"usesFoo":     true,
					"globalFoo":   true,
				},
			},
			{
				name: "*Foo.Method is referenced by usesFoo",
				toID: "*Foo.Method",
				expected: map[string]bool{
					"usesFoo": true,
				},
			},
			{
				name:     "myConst is not referenced",
				toID:     "myConst",
				expected: map[string]bool{},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := graph.IdentifiersTo(tt.toID)

				// Convert result to map for easier comparison
				resultMap := make(map[string]bool)
				for _, id := range result {
					resultMap[id] = true
				}

				// Check if all expected identifiers are present
				for expectedID := range tt.expected {
					if !resultMap[expectedID] {
						t.Errorf("Expected %s -> %s edge, but it was not found", expectedID, tt.toID)
					}
				}

				// Check if there are no unexpected identifiers
				for id := range resultMap {
					if !tt.expected[id] {
						t.Errorf("Unexpected edge %s -> %s", id, tt.toID)
					}
				}
			})
		}
	})

	// Test edge case: non-existent identifier
	t.Run("NonExistentIdentifier", func(t *testing.T) {
		fromResult := graph.IdentifiersFrom("NonExistent")
		if fromResult != nil {
			t.Errorf("Expected nil for non-existent identifier, got %v", fromResult)
		}

		toResult := graph.IdentifiersTo("NonExistent")
		if len(toResult) != 0 {
			t.Errorf("Expected empty slice for non-existent identifier, got %v", toResult)
		}
	})
}
