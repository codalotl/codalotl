package cascade

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withJSON creates a new temporary directory, writes a file named name with contents in it, and invokes callback with the file's absolute path. The file is created with 0o644 permissions.
// Name must be a relative path; the file resides under the temporary directory and will be cleaned up when the test ends. The test fails if name is absolute or if the file cannot be
// written.
func withJSON(t *testing.T, name string, contents string, callback func(path string)) {
	require.False(t, filepath.IsAbs(name))
	d := t.TempDir()
	p := filepath.Join(d, name)
	require.NoError(t, os.WriteFile(p, []byte(contents), 0o644))
	callback(p)
}

func TestSourceMap_ToMap(t *testing.T) {
	tests := []struct {
		name      string
		input     map[string]any
		expected  map[string]any
		expectErr bool
	}{
		{
			name:     "nil map returns empty map",
			input:    nil,
			expected: map[string]any{},
		},
		{
			name:     "empty map returns empty map",
			input:    map[string]any{},
			expected: map[string]any{},
		},
		{
			name:  "simple scalar key lowercased",
			input: map[string]any{"A": 1},
			expected: map[string]any{
				"a": 1,
			},
		},
		{
			name:     "allowed data types",
			input:    map[string]any{"a": 1, "b": true, "c": "str", "d": []int{1, 2, 3}, "e": []bool{true, false}, "f": []float64{1, 2, 3}, "g": []string{"a", "b", "c"}, "h": []map[string]any{{"x": 1}, {"x": 2}}, "i": nil},
			expected: map[string]any{"a": 1, "b": true, "c": "str", "d": []int{1, 2, 3}, "e": []bool{true, false}, "f": []float64{1, 2, 3}, "g": []string{"a", "b", "c"}, "h": []map[string]any{{"x": 1}, {"x": 2}}, "i": nil},
		},
		{
			name:  "dotted key becomes nested objects",
			input: map[string]any{"A.B.C": 2},
			expected: map[string]any{
				"a": map[string]any{
					"b": map[string]any{
						"c": 2,
					},
				},
			},
		},
		{
			name:  "siblings under same parent",
			input: map[string]any{"a.b": 1, "a.c": 2},
			expected: map[string]any{
				"a": map[string]any{
					"b": 1,
					"c": 2,
				},
			},
		},
		{
			name: "object value at leaf gets merged and normalized",
			input: map[string]any{
				"a.b": map[string]any{
					"C":   3,
					"d.e": 4,
				},
			},
			expected: map[string]any{
				"a": map[string]any{
					"b": map[string]any{
						"c": 3,
						"d": map[string]any{
							"e": 4,
						},
					},
				},
			},
		},
		{
			name:  "case-insensitive merge for siblings",
			input: map[string]any{"A.B": 1, "a.c": 2},
			expected: map[string]any{
				"a": map[string]any{
					"b": 1,
					"c": 2,
				},
			},
		},
		{
			name:      "conflict when scalar then nested path",
			input:     map[string]any{"a": "x", "a.b": 1},
			expectErr: true,
		},
		{
			name:      "conflict when same scalar key set twice (case-insensitive)",
			input:     map[string]any{"a": 1, "A": 2},
			expectErr: true,
		},
		{
			name:      "conflict when same scalar key set twice via different mechanism",
			input:     map[string]any{"a.b": 1, "A": map[string]any{"b": 1}},
			expectErr: true,
		},
		{
			name:      "conflict when object exists then scalar at same key",
			input:     map[string]any{"a.b": map[string]any{"c": 1}, "A.B": 2},
			expectErr: true,
		},
		{
			name:      "invalid data types - int64",
			input:     map[string]any{"a": int64(3)},
			expectErr: true,
		},
		{
			name:      "invalid data types - byte",
			input:     map[string]any{"a": byte(3)},
			expectErr: true,
		},
		{
			name:      "invalid data types - complex",
			input:     map[string]any{"a": complex(1, 1)},
			expectErr: true,
		},
		{
			name:      "invalid data types - []int64",
			input:     map[string]any{"a": []int64{1, 2, 3}},
			expectErr: true,
		},
		{
			name:      "invalid data types - map[string]string",
			input:     map[string]any{"a": map[string]string{"x": "y"}},
			expectErr: true,
		},
		{
			name:      "invalid data types - map[string]struct",
			input:     map[string]any{"a": struct{}{}},
			expectErr: true,
		},
		{
			name:      "invalid data types - slice of objects that contains and invalid type",
			input:     map[string]any{"a": []map[string]any{{"a": 1}, {"b": int64(1)}}},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := &sourceMap{m: tt.input}
			got, err := sm.ToMap()
			if tt.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestSourceJSONFile_ToMap(t *testing.T) {
	// Nonexistent file -> error
	t.Run("nonexistent file", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "nope.json")
		s := &sourceJSONFile{path: p}
		_, err := s.ToMap()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read json file")
	})

	// Empty/whitespace-only file -> empty map
	t.Run("empty file returns empty map", func(t *testing.T) {
		withJSON(t, "empty.json", "  \n\t  ", func(p string) {
			s := &sourceJSONFile{path: p}
			m, err := s.ToMap()
			require.NoError(t, err)
			assert.Equal(t, map[string]any{}, m)
		})
	})

	// Top-level must be object
	t.Run("top-level not object", func(t *testing.T) {
		withJSON(t, "arr.json", `[1,2,3]`, func(p string) {
			s := &sourceJSONFile{path: p}
			_, err := s.ToMap()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "top-level JSON must be an object")
		})
	})

	t.Run("null value allowed", func(t *testing.T) {
		withJSON(t, "null.json", `{
	  "a": null
	}`, func(p string) {
			s := &sourceJSONFile{path: p}
			m, err := s.ToMap()
			require.NoError(t, err)
			assert.Equal(t, map[string]any{"a": nil}, m)
		})
	})

	t.Run("empty array allowed", func(t *testing.T) {
		withJSON(t, "emptyarr.json", `{
	  "a": []
	}`, func(p string) {
			s := &sourceJSONFile{path: p}
			m, err := s.ToMap()
			require.NoError(t, err)
			assert.Equal(t, map[string]any{"a": []string{}}, m)
		})
	})

	// Mixed-type array
	t.Run("mixed type array not allowed", func(t *testing.T) {
		withJSON(t, "mixed.json", `{
	  "a": [1, "x"]
	}`, func(p string) {
			s := &sourceJSONFile{path: p}
			_, err := s.ToMap()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "array contains mixed types")
		})
	})

	t.Run("key collision not allowed", func(t *testing.T) {
		withJSON(t, "config.json", `{"a.b": {"c": 2}, "a": 1}`, func(p string) {
			s := &sourceJSONFile{path: p}
			_, err := s.ToMap()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "key conflict")
		})
	})

	// Happy path: scalars, arrays, nested and dotted keys, case-insensitive
	t.Run("happy path normalization", func(t *testing.T) {
		withJSON(t, "good.json", `{
			"Z": 1,
			"B": true,
			"C": "str",
			"nums": [1, 2, 3],
			"bools": [true, false],
			"strings": ["x", "y"],
			"objs": [{"X": 1}, {"X": 2}],
			"A.B.C": 2,
			"a": {"C": 3, "d.e": 4}
		}`, func(p string) {
			s := &sourceJSONFile{path: p}
			m, err := s.ToMap()
			require.NoError(t, err)
			assert.Equal(t, map[string]any{
				"a": map[string]any{
					"b": map[string]any{
						"c": float64(2),
					},
					"c": float64(3),
					"d": map[string]any{
						"e": float64(4),
					},
				},
				"z":       float64(1),
				"b":       true,
				"c":       "str",
				"nums":    []float64{1, 2, 3},
				"bools":   []bool{true, false},
				"strings": []string{"x", "y"},
				"objs":    []map[string]any{{"X": float64(1)}, {"X": float64(2)}},
			}, m)
		})
	})

	// Path expansion with ~
	t.Run("tilde path expansion", func(t *testing.T) {
		withHome(t, func(home string) {
			p := filepath.Join(home, "conf.json")
			require.NoError(t, os.WriteFile(p, []byte(`{"Key": 7}`), 0o644))
			s := &sourceJSONFile{path: "~/conf.json"}
			m, err := s.ToMap()
			require.NoError(t, err)
			assert.Equal(t, map[string]any{"key": float64(7)}, m)
		})
	})
}

func TestSourceEnv_ToMap(t *testing.T) {

	t.Run("nil mapping returns empty map", func(t *testing.T) {
		s := &sourceEnv{envToKey: nil}
		m, err := s.ToMap()
		require.NoError(t, err)
		assert.Equal(t, map[string]any{}, m)
	})

	t.Run("missing env variables are ignored", func(t *testing.T) {
		s := &sourceEnv{envToKey: map[string]string{"a": "ENV_A"}}
		_ = os.Unsetenv("ENV_A")
		m, err := s.ToMap()
		require.NoError(t, err)
		assert.Equal(t, map[string]any{}, m)
	})

	t.Run("simple mapping to string", func(t *testing.T) {
		withEnv(t, map[string]string{"ENV_A": "x"}, func() {
			s := &sourceEnv{envToKey: map[string]string{"a": "ENV_A"}}
			m, err := s.ToMap()
			require.NoError(t, err)
			assert.Equal(t, map[string]any{"a": "x"}, m)
		})
	})

	t.Run("dotted keys become nested and lowercased", func(t *testing.T) {
		withEnv(t, map[string]string{"ENV_ABC": "val"}, func() {
			s := &sourceEnv{envToKey: map[string]string{"A.B.C": "ENV_ABC"}}
			m, err := s.ToMap()
			require.NoError(t, err)
			assert.Equal(t, map[string]any{
				"a": map[string]any{
					"b": map[string]any{
						"c": "val",
					},
				},
			}, m)
		})
	})

	t.Run("empty string env value is NOT set", func(t *testing.T) {
		withEnv(t, map[string]string{"ENV_EMPTY": ""}, func() {
			s := &sourceEnv{envToKey: map[string]string{"a": "ENV_EMPTY"}}
			m, err := s.ToMap()
			require.NoError(t, err)
			assert.Equal(t, map[string]any{}, m)
		})
	})

	t.Run("siblings under same parent merge", func(t *testing.T) {
		withEnv(t, map[string]string{"ENV_B": "1", "ENV_C": "2"}, func() {
			s := &sourceEnv{envToKey: map[string]string{"a.b": "ENV_B", "a.c": "ENV_C"}}
			m, err := s.ToMap()
			require.NoError(t, err)
			assert.Equal(t, map[string]any{
				"a": map[string]any{
					"b": "1",
					"c": "2",
				},
			}, m)
		})
	})

	t.Run("conflict when same scalar key is set twice (case-insensitive)", func(t *testing.T) {
		withEnv(t, map[string]string{"ENV_X1": "one", "ENV_X2": "two"}, func() {
			s := &sourceEnv{envToKey: map[string]string{"a": "ENV_X1", "A": "ENV_X2"}}
			_, err := s.ToMap()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "conflict")
		})
	})

	t.Run("conflict when scalar and nested path collide", func(t *testing.T) {
		withEnv(t, map[string]string{"ENV_SCALAR": "x", "ENV_NESTED": "y"}, func() {
			s := &sourceEnv{envToKey: map[string]string{"a": "ENV_SCALAR", "a.b": "ENV_NESTED"}}
			_, err := s.ToMap()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "conflict")
		})
	})
}
