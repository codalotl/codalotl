package gousage

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"fmt"
	"sort"
)

type Usage struct {
	ImportPath   string
	AbsolutePath string
	RelativePath string // relative to containing module
}

// UsedBy returns a slice of sorted usages for packages that use this package within this module.
func UsedBy(pkg *gocode.Package) ([]Usage, error) {
	// NOTE: we could implement this in terms of:
	// go list -f '{{range .Imports}}{{if eq . "'"axi/codeai/tools/coretools"'"}}{{$.ImportPath}}{{"\n"}}{{end}}{{end}}' ./...
	// However, the below implementation has slightly better observed performance. 68.728458ms vs 207.141583ms on this repo at the time of writing.
	// I have an intuition that the go list approach will be more robust in the long term, since it automatically handles all corner cases
	// of Go. But for now, we'll just go with this impl.
	if pkg == nil {
		return nil, fmt.Errorf("nil package")
	}
	if pkg.Module == nil {
		return nil, fmt.Errorf("package %q has no module", pkg.ImportPath)
	}

	if err := pkg.Module.LoadAllPackages(); err != nil {
		return nil, fmt.Errorf("load module packages: %w", err)
	}

	target := pkg.ImportPath

	var usages []Usage

	for _, candidate := range pkg.Module.Packages {
		if candidate == nil {
			continue
		}
		if candidate.ImportPath == target {
			continue
		}

		if _, ok := candidate.ImportPaths[target]; ok {
			usages = append(usages, Usage{
				ImportPath:   candidate.ImportPath,
				AbsolutePath: candidate.AbsolutePath(),
				RelativePath: candidate.RelativeDir,
			})
		}

		if candidate.TestPackage != nil {
			testPkg := candidate.TestPackage
			if _, ok := testPkg.ImportPaths[target]; ok {
				usages = append(usages, Usage{
					ImportPath:   testPkg.ImportPath,
					AbsolutePath: testPkg.AbsolutePath(),
					RelativePath: testPkg.RelativeDir,
				})
			}
		}
	}

	sort.Slice(usages, func(i, j int) bool {
		return usages[i].ImportPath < usages[j].ImportPath
	})

	return usages, nil
}
