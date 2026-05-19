package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	repoRoot, err := nearestGitRepoRoot(wd)
	if err != nil {
		return err
	}

	pkgDirs, err := goListPackageDirsUnderRepo(ctx, repoRoot)
	if err != nil {
		return err
	}

	var stale []string
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

		display, summary, ok, err := summarizeCASPackageFromBase(repoRoot, pkgDir.mod, db, spec, pkgDir.absDir)
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

type casRepoPackageDir struct {
	absDir string
	mod    *gocode.Module
}

func nearestGitRepoRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		dir = filepath.Dir(dir)
	}

	for {
		_, err := os.Stat(filepath.Join(dir, ".git"))
		switch {
		case err == nil:
			return dir, nil
		case !os.IsNotExist(err):
			return "", err
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no enclosing git repository found from %q", start)
		}
		dir = parent
	}
}

func goListPackageDirsUnderRepo(ctx context.Context, repoRoot string) ([]casRepoPackageDir, error) {
	mods, err := gocode.DiscoverModules(repoRoot)
	if err != nil {
		return nil, err
	}

	seen := map[string]casRepoPackageDir{}
	for _, mod := range mods {
		dirs, err := goListPackageDirsFromDir(ctx, mod.AbsolutePath, "./...")
		if err != nil {
			return nil, err
		}
		for _, absDir := range dirs {
			if _, ok := displayPackagePath(repoRoot, absDir); !ok {
				continue
			}
			if _, ok := seen[absDir]; ok {
				continue
			}
			seen[absDir] = casRepoPackageDir{
				absDir: absDir,
				mod:    mod,
			}
		}
	}

	out := make([]casRepoPackageDir, 0, len(seen))
	for _, pkgDir := range seen {
		out = append(out, pkgDir)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].absDir < out[j].absDir
	})
	return out, nil
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
