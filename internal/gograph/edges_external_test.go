package gograph

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExternalIdentifiersFrom verifies that ExternalIdentifiersFrom correctly filters cross-package references depending on the includeVendor / includeStdlib flags.
func TestExternalIdentifiersFrom(t *testing.T) {
	// Source code for the main test package. It references:
	//   * stdlib package "strings" (ToUpper)
	//   * third-party package "github.com/example/venpkg" (DoSomething)
	//   * another package within the same module "test/otherpkg" (OtherFunc)
	srcMain := dedent(`
        import (
            "strings"
            ven "github.com/example/venpkg"
            "test/otherpkg"
        )

        func UseAll() {
            _ = strings.ToUpper("hi")
            ven.DoSomething()
            otherpkg.OtherFunc()
        }
    `)

	// We do NOT need real Go code for the vendored or other module packages –
	// the Graph uses a stub importer for any non-stdlib import paths. However,
	// we create a minimal source file for otherpkg so that "go/packages" can
	// successfully load it when categorising import paths.
	srcOtherPkg := `package otherpkg
    // Exported so selector expression can reference it.
    func OtherFunc() {}
    `

	files := map[string]string{
		"main.go": srcMain,
		// Note: write the other package file into a separate directory at build time (see below).
	}

	pkg := newTestPackageWithFiles(t, files)

	// After the primary package is created, add the otherpkg source file to the
	// module so that it is discoverable under the same module path.
	otherDir := filepath.Join(pkg.Module.AbsolutePath, "otherpkg")
	require.NoError(t, os.Mkdir(otherDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(otherDir, "other.go"), []byte(srcOtherPkg), 0o644))

	graph, err := NewGoGraph(pkg)
	require.NoError(t, err)

	// Helper to convert slice to deterministic, sorted form for comparison.
	sortRefs := func(refs []ExternalID) []ExternalID {
		out := make([]ExternalID, len(refs))
		copy(out, refs)
		sort.Slice(out, func(i, j int) bool {
			if out[i].ImportPath == out[j].ImportPath {
				return out[i].ID < out[j].ID
			}
			return out[i].ImportPath < out[j].ImportPath
		})
		return out
	}

	// Expected references (unfiltered).
	allRefs := []ExternalID{
		{ImportPath: "strings", ID: "ToUpper"},
		{ImportPath: "github.com/example/venpkg", ID: "DoSomething"},
		{ImportPath: "test/otherpkg", ID: "OtherFunc"},
	}

	tests := []struct {
		name           string
		includeVendor  bool
		includeStdlib  bool
		expectedSubset []ExternalID
	}{
		{
			name:           "module only",
			includeVendor:  false,
			includeStdlib:  false,
			expectedSubset: []ExternalID{{ImportPath: "test/otherpkg", ID: "OtherFunc"}},
		},
		{
			name:           "module + vendor",
			includeVendor:  true,
			includeStdlib:  false,
			expectedSubset: []ExternalID{{ImportPath: "github.com/example/venpkg", ID: "DoSomething"}, {ImportPath: "test/otherpkg", ID: "OtherFunc"}},
		},
		{
			name:           "module + stdlib",
			includeVendor:  false,
			includeStdlib:  true,
			expectedSubset: []ExternalID{{ImportPath: "strings", ID: "ToUpper"}, {ImportPath: "test/otherpkg", ID: "OtherFunc"}},
		},
		{
			name:           "all",
			includeVendor:  true,
			includeStdlib:  true,
			expectedSubset: allRefs,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs := graph.ExternalIdentifiersFrom("UseAll", tt.includeVendor, tt.includeStdlib)
			assert.Equal(t, sortRefs(tt.expectedSubset), sortRefs(refs))
		})
	}
}
