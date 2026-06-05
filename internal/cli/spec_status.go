package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocas/casconformance"
	"github.com/codalotl/codalotl/internal/specmd"
)

// A specStatusRow contains SPEC.md and conformance status for one package.
type specStatusRow struct {
	Package  string // Display package path.
	HasSpec  string // Whether the package has a SPEC.md file.
	APIMatch string // Whether the SPEC.md public API matches the implementation.
	Conforms string // Stored CAS conformance status for the current package contents.
}

// runSpecStatus writes per-package SPEC.md status for Go modules under the nearest Git repository. It is read-only: it reports SPEC.md presence, public API match
// status, and current CAS conformance status, sorted for the status command. Package-level SPEC or CAS failures are represented in the table when possible; discovery,
// database, and output failures are returned as errors.
func runSpecStatus(ctx context.Context, out io.Writer) error {
	repoRoot, pkgDirs, err := goListPackageDirsUnderNearestGitRepo(ctx)
	if err != nil {
		return err
	}

	rows := make([]specStatusRow, 0, len(pkgDirs))
	dbs := map[string]*gocas.DB{}
	for _, pkgDir := range pkgDirs {
		display, ok := displayPackagePath(repoRoot, pkgDir.absDir)
		if !ok {
			continue
		}

		moduleRoot := pkgDir.mod.AbsolutePath
		db, err := cachedCASReadDBForBaseDir(dbs, moduleRoot)
		if err != nil {
			return err
		}

		row := specStatusRow{
			Package:  display,
			HasSpec:  "false",
			APIMatch: "-",
			Conforms: "unset",
		}

		specPath := filepath.Join(pkgDir.absDir, "SPEC.md")
		if info, err := os.Stat(specPath); err == nil && !info.IsDir() {
			row.HasSpec = "true"
			match, err := specMatchesPublicAPI(specPath)
			if err != nil {
				row.APIMatch = "error"
			} else if match {
				row.APIMatch = "true"
			} else {
				row.APIMatch = "false"
			}
		}

		pkg, err := loadPackageFromRepoDir(pkgDir)
		if err != nil {
			row.Conforms = "error"
			rows = append(rows, row)
			continue
		}
		found, conforms, err := casconformance.Retrieve(db, pkg)
		if err != nil {
			row.Conforms = "error"
			rows = append(rows, row)
			continue
		}
		if found {
			if conforms {
				row.Conforms = "true"
			} else {
				row.Conforms = "false"
			}
		}

		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.HasSpec != b.HasSpec {
			return boolRankTrueFirst(a.HasSpec) < boolRankTrueFirst(b.HasSpec)
		}
		if a.APIMatch != b.APIMatch {
			return apiMatchRank(a.APIMatch) < apiMatchRank(b.APIMatch)
		}
		if a.Conforms != b.Conforms {
			return conformsRank(a.Conforms) < conformsRank(b.Conforms)
		}
		return a.Package < b.Package
	})

	return writeSpecStatusTable(out, rows)
}

func specMatchesPublicAPI(specPath string) (bool, error) {
	spec, err := specmd.Read(specPath)
	if err != nil {
		return false, err
	}
	diffs, err := spec.ImplementationDiffs()
	if err != nil {
		return false, err
	}
	return len(diffs) == 0, nil
}

// writeSpecStatusTable writes rows as an aligned SPEC status table.
func writeSpecStatusTable(w io.Writer, rows []specStatusRow) error {
	tableRows := make([][]string, 0, len(rows))
	for _, r := range rows {
		tableRows = append(tableRows, []string{r.Package, r.HasSpec, r.APIMatch, r.Conforms})
	}
	return writeAlignedTable(w, []string{"package", "has_spec", "api_match", "conforms"}, tableRows)
}

func boolRankTrueFirst(v string) int {
	switch v {
	case "true":
		return 0
	case "false":
		return 1
	default:
		return 2
	}
}

func apiMatchRank(v string) int {
	switch v {
	case "true":
		return 0
	case "false":
		return 1
	case "error":
		return 2
	case "-":
		return 3
	default:
		return 4
	}
}

func conformsRank(v string) int {
	switch v {
	case "true":
		return 0
	case "false":
		return 1
	case "unset":
		return 2
	case "error":
		return 3
	default:
		return 4
	}
}
