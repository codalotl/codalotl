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
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
)

type casSummaryRow struct {
	Package      string
	UpToDate     string
	Stale        string
	Age          string
	ChurnPercent string
	state        casPackageState
	age          *time.Duration
	churn        *float64
}

type casPackageState string

const (
	casPackageStateAll      casPackageState = "all"
	casPackageStateCurrent  casPackageState = "current"
	casPackageStateOutdated casPackageState = "outdated"
	casPackageStateStale    casPackageState = "stale"
	casPackageStateMissing  casPackageState = "missing"
)

type casLsPackagesOptions struct {
	OutputCSV     bool
	State         casPackageState
	StateExplicit bool
	MinAge        *time.Duration
	MinChurn      *float64
}

func parseCASLsPackagesOptions(outputCSV bool, state string, minAge string, minChurn string) (casLsPackagesOptions, error) {
	opts := casLsPackagesOptions{
		OutputCSV:     outputCSV,
		State:         casPackageStateAll,
		StateExplicit: strings.TrimSpace(state) != "",
	}

	if opts.StateExplicit {
		parsedState, err := parseCASPackageState(state)
		if err != nil {
			return casLsPackagesOptions{}, err
		}
		opts.State = parsedState
	}

	if strings.TrimSpace(minAge) != "" {
		age, err := parseCASLsPackagesMinAge(minAge)
		if err != nil {
			return casLsPackagesOptions{}, err
		}
		opts.MinAge = &age
	}

	if strings.TrimSpace(minChurn) != "" {
		churn, err := parseCASLsPackagesMinChurn(minChurn)
		if err != nil {
			return casLsPackagesOptions{}, err
		}
		opts.MinChurn = &churn
	}

	if !opts.StateExplicit && (opts.MinAge != nil || opts.MinChurn != nil) {
		opts.State = casPackageStateStale
	}

	return opts, nil
}

func parseCASPackageState(state string) (casPackageState, error) {
	switch casPackageState(strings.TrimSpace(state)) {
	case casPackageStateAll:
		return casPackageStateAll, nil
	case casPackageStateCurrent:
		return casPackageStateCurrent, nil
	case casPackageStateOutdated:
		return casPackageStateOutdated, nil
	case casPackageStateStale:
		return casPackageStateStale, nil
	case casPackageStateMissing:
		return casPackageStateMissing, nil
	default:
		return "", qcli.UsageError{Message: fmt.Sprintf("invalid --state: expected all, current, outdated, stale, or missing (got %q)", state)}
	}
}

func parseCASLsPackagesMinAge(s string) (time.Duration, error) {
	raw := strings.TrimSpace(s)
	usageErr := func() error {
		return qcli.UsageError{Message: fmt.Sprintf("invalid --min-age: expected duration like 12h, 30d, 4w, or 1y (got %q)", s)}
	}
	if raw == "" {
		return 0, qcli.UsageError{Message: "invalid --min-age: empty duration"}
	}
	unit := raw[len(raw)-1]
	if unit == 'd' || unit == 'w' || unit == 'y' {
		n, err := strconv.ParseInt(strings.TrimSpace(raw[:len(raw)-1]), 10, 64)
		if err != nil || n < 0 {
			return 0, usageErr()
		}
		var multiplier time.Duration
		switch unit {
		case 'd':
			multiplier = 24 * time.Hour
		case 'w':
			multiplier = 7 * 24 * time.Hour
		case 'y':
			multiplier = 365 * 24 * time.Hour
		}
		const maxDuration = time.Duration(1<<63 - 1)
		if n > int64(maxDuration/multiplier) {
			return 0, usageErr()
		}
		return time.Duration(n) * multiplier, nil
	}

	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return 0, usageErr()
	}
	return d, nil
}

func parseCASLsPackagesMinChurn(s string) (float64, error) {
	raw := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	if raw == "" {
		return 0, qcli.UsageError{Message: "invalid --min-churn: empty percent"}
	}
	churn, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(churn) || math.IsInf(churn, 0) || churn < 0 {
		return 0, qcli.UsageError{Message: fmt.Sprintf("invalid --min-churn: expected percent like 20 or 20%% (got %q)", s)}
	}
	return churn, nil
}

func runCASLsPackages(ctx context.Context, out io.Writer, namespace string, opts casLsPackagesOptions) error {
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
		if ok && casSummaryRowMatchesFilters(row, opts) {
			rows = append(rows, row)
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Package < rows[j].Package
	})

	if opts.OutputCSV {
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
		UpToDate:     "no",
		Stale:        "no",
		Age:          "-",
		ChurnPercent: "-",
		state:        casPackageStateMissing,
	}
	if summary.Current != nil {
		row.UpToDate = "yes"
		row.Stale = "-"
		row.state = casPackageStateCurrent
		row.age = casSummaryAgeDuration(summary.Current.Time, now)
		row.Age = formatCASSummaryAge(summary.Current.Time, now)
		return row, true, nil
	}
	if summary.PriorInvalidated != nil {
		row.Stale = "yes"
		row.state = casPackageStateStale
		row.age = casSummaryAgeDuration(summary.PriorInvalidated.Time, now)
		row.Age = formatCASSummaryAge(summary.PriorInvalidated.Time, now)
		if summary.ChurnPercent != nil {
			churn := *summary.ChurnPercent
			row.churn = &churn
			row.ChurnPercent = formatCASSummaryChurn(*summary.ChurnPercent)
		}
	}
	return row, true, nil
}

func casSummaryRowMatchesFilters(row casSummaryRow, opts casLsPackagesOptions) bool {
	if !casPackageStateMatches(row.state, opts.State) {
		return false
	}

	hasThreshold := opts.MinAge != nil || opts.MinChurn != nil
	if opts.StateExplicit && opts.State == casPackageStateOutdated && row.state == casPackageStateMissing && hasThreshold {
		return true
	}

	if opts.MinAge != nil {
		if row.age == nil || *row.age < *opts.MinAge {
			return false
		}
	}
	if opts.MinChurn != nil {
		if row.churn == nil || *row.churn < *opts.MinChurn {
			return false
		}
	}
	return true
}

func casPackageStateMatches(rowState casPackageState, filterState casPackageState) bool {
	switch filterState {
	case casPackageStateAll:
		return true
	case casPackageStateCurrent:
		return rowState == casPackageStateCurrent
	case casPackageStateOutdated:
		return rowState == casPackageStateStale || rowState == casPackageStateMissing
	case casPackageStateStale:
		return rowState == casPackageStateStale
	case casPackageStateMissing:
		return rowState == casPackageStateMissing
	default:
		return false
	}
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
	if _, err := fmt.Fprintln(tw, "Package\tUp to date\tStale\tAge\tChurn %"); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", row.Package, row.UpToDate, row.Stale, row.Age, row.ChurnPercent); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func writeCASSummaryCSV(w io.Writer, rows []casSummaryRow) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"Package", "Up to date", "Stale", "Age", "Churn %"}); err != nil {
		return err
	}
	for _, row := range rows {
		if err := cw.Write([]string{row.Package, row.UpToDate, row.Stale, row.Age, row.ChurnPercent}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func formatCASSummaryAge(t time.Time, now time.Time) string {
	d := casSummaryAgeDuration(t, now)
	if d == nil {
		return "-"
	}
	return formatCASSummaryDuration(*d)
}

func casSummaryAgeDuration(t time.Time, now time.Time) *time.Duration {
	if t.IsZero() {
		return nil
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	return &d
}

func formatCASSummaryDuration(d time.Duration) string {
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
