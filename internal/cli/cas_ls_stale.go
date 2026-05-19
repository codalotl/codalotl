package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
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

	db, err := casReadDBForBaseDir(repoRoot)
	if err != nil {
		return err
	}

	var stale []string
	mods := map[string]*gocode.Module{}
	now := time.Now()
	for _, pkgDir := range pkgDirs {
		mod, ok := mods[pkgDir.moduleRoot]
		if !ok {
			var err error
			mod, err = gocode.NewModule(pkgDir.moduleRoot)
			if err != nil {
				return err
			}
			mods[pkgDir.moduleRoot] = mod
		}

		display, summary, ok, err := summarizeCASPackageFromBase(repoRoot, mod, db, spec, pkgDir.absDir)
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
	absDir     string
	moduleRoot string
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
	moduleRoots, err := goModuleRootsUnderRepo(repoRoot)
	if err != nil {
		return nil, err
	}

	seen := map[string]casRepoPackageDir{}
	for _, moduleRoot := range moduleRoots {
		dirs, err := goListPackageDirsFromDir(ctx, moduleRoot, "./...")
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
				absDir:     absDir,
				moduleRoot: moduleRoot,
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

func goModuleRootsUnderRepo(repoRoot string) ([]string, error) {
	var roots []string
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		if path != repoRoot {
			switch d.Name() {
			case ".git", ".codalotl", "vendor":
				return filepath.SkipDir
			}
		}
		_, err := os.Stat(filepath.Join(path, "go.mod"))
		switch {
		case err == nil:
			roots = append(roots, path)
		case os.IsNotExist(err):
		default:
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(roots)
	return roots, nil
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
