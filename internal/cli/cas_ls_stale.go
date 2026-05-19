package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
)

const (
	defaultCASLsStaleAfterDays       = 30
	defaultCASLsStaleMinChurnPercent = 20
	maxCASLsStaleAfterDays           = int64(1<<63-1) / int64(24*time.Hour)
)

func runCASLsStale(ctx context.Context, out io.Writer, namespace string, staleAfterDays int, minChurnPercent int) error {
	if err := validateCASLsStaleThresholds(staleAfterDays, minChurnPercent); err != nil {
		return err
	}

	spec, err := resolveCASNamespaceSpec(namespace)
	if err != nil {
		return qcli.UsageError{Message: err.Error()}
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	mod, err := gocode.NewModule(wd)
	if err != nil {
		return err
	}

	// Consider packages in the module based on cwd.
	pkgDirs, err := goListPackageDirsFromDir(ctx, mod.AbsolutePath, "./...")
	if err != nil {
		return err
	}

	db, err := casReadDBForBaseDir(mod.AbsolutePath)
	if err != nil {
		return err
	}

	var stale []string
	now := time.Now()
	for _, absPkgDir := range pkgDirs {
		display, summary, ok, err := summarizeCASPackage(mod, db, spec, absPkgDir)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if casPackageSummaryIsStale(summary, now, staleAfterDays, minChurnPercent) {
			stale = append(stale, display)
		}
	}

	sort.Strings(stale)
	for _, line := range stale {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	return nil
}

func validateCASLsStaleThresholds(staleAfterDays int, minChurnPercent int) error {
	if staleAfterDays < 0 {
		return qcli.UsageError{Message: fmt.Sprintf("invalid --stale-after-days: must be >= 0 (got %d)", staleAfterDays)}
	}
	if int64(staleAfterDays) > maxCASLsStaleAfterDays {
		return qcli.UsageError{Message: fmt.Sprintf("invalid --stale-after-days: must be <= %d (got %d)", maxCASLsStaleAfterDays, staleAfterDays)}
	}
	if minChurnPercent < 0 {
		return qcli.UsageError{Message: fmt.Sprintf("invalid --min-churn-percent: must be >= 0 (got %d)", minChurnPercent)}
	}
	return nil
}

func casPackageSummaryIsStale(summary gocas.PackageSummary, now time.Time, staleAfterDays int, minChurnPercent int) bool {
	if summary.Current != nil {
		return false
	}
	if summary.PriorInvalidated == nil {
		return true
	}
	if !summary.PriorInvalidated.Time.IsZero() {
		age := now.Sub(summary.PriorInvalidated.Time)
		if age >= 0 && age >= time.Duration(staleAfterDays)*24*time.Hour {
			return true
		}
	}
	if summary.ChurnPercent != nil && *summary.ChurnPercent >= float64(minChurnPercent) {
		return true
	}
	return false
}

func goListPackageDirsFromDir(ctx context.Context, dir string, pattern string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-e", "-f", "{{.Dir}}", pattern)
	cmd.Dir = dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	dirs := parseNonEmptyLines(stdout.Bytes())
	if err != nil && len(dirs) == 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("go list %q: %s", pattern, msg)
		}
		return nil, fmt.Errorf("go list %q: %w", pattern, err)
	}
	uniq := map[string]struct{}{}
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if _, ok := uniq[d]; ok {
			continue
		}
		uniq[d] = struct{}{}
		out = append(out, d)
	}
	sort.Strings(out)
	return out, nil
}
