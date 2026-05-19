package cli

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
)

type casSummaryRow struct {
	Package      string
	CAS          string
	PrevCAS      string
	Age          string
	ChurnPercent string
}

func runCASLsSummary(ctx context.Context, out io.Writer, namespace string, outputCSV bool) error {
	spec, err := resolveCASNamespaceSpec(namespace)
	if err != nil {
		return qcli.UsageError{Message: err.Error()}
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	repoRoot, err := nearestGitRepoRoot(wd)
	if err != nil {
		return err
	}

	pkgDirs, err := goListPackageDirsUnderRepo(ctx, repoRoot)
	if err != nil {
		return err
	}

	rows := make([]casSummaryRow, 0, len(pkgDirs))
	dbs := map[string]*gocas.DB{}
	now := time.Now()
	for _, pkgDir := range pkgDirs {
		moduleRoot := pkgDir.mod.AbsolutePath
		db, ok := dbs[moduleRoot]
		if !ok {
			var err error
			db, err = casReadDBForBaseDir(moduleRoot)
			if err != nil {
				return err
			}
			dbs[moduleRoot] = db
		}

		row, ok, err := casSummaryRowForPackage(repoRoot, pkgDir.mod, db, spec, pkgDir.absDir, now)
		if err != nil {
			return err
		}
		if ok {
			rows = append(rows, row)
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Package < rows[j].Package
	})

	if outputCSV {
		return writeCASSummaryCSV(out, rows)
	}
	return writeCASSummaryTable(out, rows)
}

func casSummaryRowForPackage(displayBaseDir string, mod *gocode.Module, db *gocas.DB, spec gocas.NamespaceSpec, absPkgDir string, now time.Time) (casSummaryRow, bool, error) {
	display, summary, ok, err := summarizeCASPackageFromBase(displayBaseDir, mod, db, spec, absPkgDir)
	if err != nil || !ok {
		return casSummaryRow{}, ok, err
	}

	row := casSummaryRow{
		Package:      display,
		CAS:          "no",
		PrevCAS:      "no",
		Age:          "-",
		ChurnPercent: "-",
	}
	if summary.Current != nil {
		row.CAS = "yes"
		row.PrevCAS = "-"
		row.Age = formatCASSummaryAge(summary.Current.Time, now)
		return row, true, nil
	}
	if summary.PriorInvalidated != nil {
		row.PrevCAS = "yes"
		row.Age = formatCASSummaryAge(summary.PriorInvalidated.Time, now)
		if summary.ChurnPercent != nil {
			row.ChurnPercent = formatCASSummaryChurn(*summary.ChurnPercent)
		}
	}
	return row, true, nil
}

func summarizeCASPackageFromBase(displayBaseDir string, mod *gocode.Module, db *gocas.DB, spec gocas.NamespaceSpec, absPkgDir string) (string, gocas.PackageSummary, bool, error) {
	display, ok := displayPackagePath(displayBaseDir, absPkgDir)
	if !ok {
		return "", gocas.PackageSummary{}, false, nil
	}

	rel, err := filepath.Rel(mod.AbsolutePath, absPkgDir)
	if err != nil {
		return "", gocas.PackageSummary{}, false, nil
	}

	pkg, err := mod.LoadPackageByRelativeDir(rel)
	if err != nil {
		return "", gocas.PackageSummary{}, false, nil
	}

	summary, err := db.SummarizePackage(pkg, spec)
	if err != nil {
		return "", gocas.PackageSummary{}, false, err
	}
	return display, summary, true, nil
}

func writeCASSummaryTable(w io.Writer, rows []casSummaryRow) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "Package\tCAS\tPrev CAS\tAge\tChurn %"); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", row.Package, row.CAS, row.PrevCAS, row.Age, row.ChurnPercent); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, "Note: Prev CAS '-' with CAS=yes means not applicable (current CAS exists), not false/no previous record.")
	return err
}

func writeCASSummaryCSV(w io.Writer, rows []casSummaryRow) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"Package", "CAS", "Prev CAS", "Age", "Churn %"}); err != nil {
		return err
	}
	for _, row := range rows {
		if err := cw.Write([]string{row.Package, row.CAS, row.PrevCAS, row.Age, row.ChurnPercent}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func formatCASSummaryAge(t time.Time, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d >= 365*24*time.Hour:
		return fmt.Sprintf("%dy", int(d/(365*24*time.Hour)))
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	default:
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
}

func formatCASSummaryChurn(churn float64) string {
	if churn < 0 {
		churn = 0
	}
	return fmt.Sprintf("%.0f%%", math.Round(churn))
}
