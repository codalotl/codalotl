package gocode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupFunctionsByType(t *testing.T) {
	tests := []struct {
		name          string
		sourceCode    string
		expectedTypes map[string][]string // map of type name to slice of function names
	}{
		{
			name: "functions with receivers",
			sourceCode: `
				type Foo struct{}
				func (f *Foo) Method1() {}
				func (f Foo) Method2() {}
				func RegularFunc() {}
			`,
			expectedTypes: map[string][]string{
				"Foo":  {"Method1", "Method2"},
				"none": {"RegularFunc"},
			},
		},
		{
			name: "functions returning types",
			sourceCode: `
				type Bar struct{}
				func NewBar() Bar { return Bar{} }
				func NewBarPtr() *Bar { return &Bar{} }
				func CreateBarWithError() (Bar, error) { return Bar{}, nil }
			`,
			expectedTypes: map[string][]string{
				"Bar":  {"NewBar", "NewBarPtr", "CreateBarWithError"},
				"none": {},
			},
		},
		{
			name: "unexported receiver types",
			sourceCode: `
				type Baz struct{}
				type unexported struct{}
				
				func (b Baz) Method() {}
				func (u unexported) Method() {}
			`,
			expectedTypes: map[string][]string{
				"Baz":        {"Method"},
				"unexported": {"Method"},
			},
		},
		{
			name: "multiple return types",
			sourceCode: `
				type Result struct{}
				type Context struct{}
				
				func Process() (Result, Context) { return Result{}, Context{} }
				func ProcessWithBuiltIn() (Result, error) { return Result{}, nil }
			`,
			expectedTypes: map[string][]string{
				"Result":  {"ProcessWithBuiltIn"},
				"none":    {"Process"},
				"Context": {},
			},
		},
		{
			name: "slice",
			sourceCode: `
				type Result struct{}
				func R1() []Result
				func R2() []*Result
			`,
			expectedTypes: map[string][]string{
				"Result": {"R1", "R2"},
			},
		},
		{
			name: "multiple return types - builtins and other packages",
			sourceCode: `
				type Result struct{}
				type Context struct{}
				
				func R1() Result
				func U1() (Result, Context)
				func C1() Context
				func R2() (Result, error)
				func R3() (int, Result, error)
				func U2() (int, Result, error, Context)
				func R4() (*int, *Result, []error)
				func R5() (Result, io.Reader)
				func R6() (Result, other.T)
				func R7() (other.T, []Result)
				func U3() (other.T, Result, Context)
			`,
			expectedTypes: map[string][]string{
				"Result":  {"R1", "R2", "R3", "R4", "R5", "R6", "R7"},
				"none":    {"U1", "U2", "U3"},
				"Context": {"C1"},
			},
		},
		{
			name: "multiple return types 2",
			sourceCode: `
				type Result struct{}
				type Context struct{}
				type idea struct{}

				func G1() (Result, Context) { return Result{}, Context{} }
				func F1() (Result, error, int) { return Result{}, nil, 0 }
				func F2() (int, Result, error) { return 0, Result{}, nil }
				func G2() (int, Result, Context, error) { return 0, Result{}, Context{}, nil }
				func G3() (Result, idea) { return Result{}, idea{} }
			`,
			expectedTypes: map[string][]string{
				"Result":  {"F1", "F2"},
				"none":    {"G1", "G2", "G3"},
				"Context": {},
			},
		},
		{
			name: "returns T and other package types",
			sourceCode: `
				type Widget struct{}
				type Gadget struct{}
				type Component struct{}
				
				// Should be grouped with Widget (returns Widget + built-ins)
				func NewWidget() (*Widget, error) { return nil, nil }
				func GetWidget() (Widget, string, int) { return Widget{}, "", 0 }

				// Should NOT be grouped (returns multiple types from same pkg)
				func MakeWidgetAndGadget() (Widget, Gadget) { return Widget{}, Gadget{} }
				func BuildAll() (*Widget, *Gadget, error) { return nil, nil, nil }
				func CreatePair() (Widget, *Component) { return Widget{}, nil }

				// Should be grouped with Gadget (only Gadget from pkg)
				func NewGadget() (Gadget, bool) { return Gadget{}, true }
				func LoadGadget() (*Gadget, int, error) { return nil, 0, nil }
			`,
			expectedTypes: map[string][]string{
				"Widget":    {"NewWidget", "GetWidget"},
				"Gadget":    {"NewGadget", "LoadGadget"},
				"Component": {},
				"none":      {"MakeWidgetAndGadget", "BuildAll", "CreatePair"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Extract snippets using the helper function
			funcs, _, types, _, err := extractSnippetsFromSource(t, tt.sourceCode)
			require.NoError(t, err)

			// Call groupFunctionsByType
			result := groupFunctionsByType(types, funcs)

			// Convert result to map of type to function names for easier assertion
			resultNames := make(map[string][]string)
			for typeName, funcs := range result {
				for _, fn := range funcs {
					resultNames[typeName] = append(resultNames[typeName], fn.Name)
				}
			}

			// Check that each expected type has the expected functions
			for typeName, expectedFuncs := range tt.expectedTypes {
				assert.ElementsMatch(t, expectedFuncs, resultNames[typeName],
					"Functions for type %q don't match expected", typeName)
			}

			// Check no unexpected types
			for typeName := range resultNames {
				_, ok := tt.expectedTypes[typeName]
				assert.True(t, ok, "Unexpected type in result: %q", typeName)
			}
		})
	}
}
