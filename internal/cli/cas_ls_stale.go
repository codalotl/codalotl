package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codalotl/codalotl/internal/gocode"
)

// casRepoPackageDir identifies a Go package directory discovered under a repo.
type casRepoPackageDir struct {
	absDir string         // Absolute directory of the package on disk.
	mod    *gocode.Module // Module that contains the package directory.
}

// nearestGitRepoRoot returns the nearest enclosing Git repository root for start.
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

// goListPackageDirsUnderRepo returns the Go package directories in modules discovered under repoRoot. It omits packages outside repoRoot, deduplicates package directories
// found through multiple modules, and returns results sorted by absolute directory. The context controls the underlying go list commands.
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

// goListPackageDirsFromDir returns sorted unique package directories matching pattern from dir. It runs `go list -e -f {{.Dir}}` with dir as the command working
// directory, with ctx controlling the command lifetime. If go list reports an error after producing no directories, the returned error includes stderr when available.
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
