package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codalotl/codalotl/internal/gocas/casconformance"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/specmd"
)

type specStatusRow struct {
	Package  string
	HasSpec  string
	APIMatch string
	Conforms string
}

func runSpecStatus(ctx context.Context, out io.Writer) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	mod, err := gocode.NewModule(wd)
	if err != nil {
		return err
	}

	// NOTE: Spec status is intentionally based on ./... rooted at the current
	// working directory (per SPEC.md). Paths printed are module-relative.
	pkgDirs, err := goListPackageDirs(ctx, "./...")
	if err != nil {
		return err
	}
	db, err := casDBForBaseDir(mod.AbsolutePath)
	if err != nil {
		return err
	}

	rows := make([]specStatusRow, 0, len(pkgDirs))
	for _, absPkgDir := range pkgDirs {
		display, ok := displayPackagePath(mod.AbsolutePath, absPkgDir)
		if !ok {
			continue
		}

		row := specStatusRow{
			Package:  display,
			HasSpec:  "false",
			APIMatch: "-",
			Conforms: "unset",
		}

		specPath := filepath.Join(absPkgDir, "SPEC.md")
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

		relDir, err := filepath.Rel(mod.AbsolutePath, absPkgDir)
		if err != nil {
			row.Conforms = "error"
			rows = append(rows, row)
			continue
		}
		if relDir == ".." || strings.HasPrefix(relDir, ".."+string(filepath.Separator)) {
			// Shouldn't happen (go list should remain within the module graph),
			// but treat as an error if it does.
			row.Conforms = "error"
			rows = append(rows, row)
			continue
		}
		pkg, err := mod.LoadPackageByRelativeDir(relDir)
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

func writeSpecStatusTable(w io.Writer, rows []specStatusRow) error {
	cols := [][]string{
		{"package"},
		{"has_spec"},
		{"api_match"},
		{"conforms"},
	}
	for _, r := range rows {
		cols[0] = append(cols[0], r.Package)
		cols[1] = append(cols[1], r.HasSpec)
		cols[2] = append(cols[2], r.APIMatch)
		cols[3] = append(cols[3], r.Conforms)
	}

	widths := make([]int, len(cols))
	for i := range cols {
		for _, v := range cols[i] {
			if n := len(v); n > widths[i] {
				widths[i] = n
			}
		}
	}

	writeRow := func(values ...string) error {
		for i, v := range values {
			if i > 0 {
				if _, err := io.WriteString(w, "  "); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(w, "%-*s", widths[i], v); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, "\n")
		return err
	}

	if err := writeRow("package", "has_spec", "api_match", "conforms"); err != nil {
		return err
	}
	if err := writeRow(
		strings.Repeat("-", widths[0]),
		strings.Repeat("-", widths[1]),
		strings.Repeat("-", widths[2]),
		strings.Repeat("-", widths[3]),
	); err != nil {
		return err
	}
	for _, r := range rows {
		if err := writeRow(r.Package, r.HasSpec, r.APIMatch, r.Conforms); err != nil {
			return err
		}
	}
	return nil
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
