package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
)

const defaultCASPruneDays = 30

// casPrunePackageGroup groups packages that share one module CAS database.
type casPrunePackageGroup struct {
	mod      *gocode.Module    // Module is the module whose CAS database is pruned.
	packages []*gocode.Package // Packages are the module packages included in the prune operation.
}

// runCASPrune deletes obsolete CAS records for packages in modules under the nearest Git repository. It removes prior namespace versions and superseded package
// records older than days, writes a deletion summary to out, and returns an error for invalid days, package discovery, CAS, or output failures. A days value of
// zero uses gocas's default retention period.
func runCASPrune(ctx context.Context, out io.Writer, days int) error {
	if err := validateCASPruneDays(days); err != nil {
		return err
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

	groups := map[string]*casPrunePackageGroup{}
	for _, pkgDir := range pkgDirs {
		moduleRoot := pkgDir.mod.AbsolutePath
		group, ok := groups[moduleRoot]
		if !ok {
			group = &casPrunePackageGroup{mod: pkgDir.mod}
			groups[moduleRoot] = group
		}

		rel, err := filepath.Rel(moduleRoot, pkgDir.absDir)
		if err != nil {
			return err
		}
		pkg, err := pkgDir.mod.LoadPackageByRelativeDir(rel)
		if err != nil {
			return err
		}
		group.packages = append(group.packages, pkg)
	}

	moduleRoots := make([]string, 0, len(groups))
	for moduleRoot := range groups {
		moduleRoots = append(moduleRoots, moduleRoot)
	}
	sort.Strings(moduleRoots)

	specs := sortedCASNamespaceSpecs()
	var total gocas.PruneResult
	for _, moduleRoot := range moduleRoots {
		group := groups[moduleRoot]
		db, err := casReadDBForBaseDir(group.mod.AbsolutePath)
		if err != nil {
			return err
		}
		result, err := db.Prune(specs, group.packages, gocas.PruneOptions{SupersededAgeDays: days})
		if err != nil {
			return err
		}
		total.DeletedPriorVersionRecords += result.DeletedPriorVersionRecords
		total.DeletedSupersededRecords += result.DeletedSupersededRecords
	}

	return writeCASPruneSummary(out, total)
}

func validateCASPruneDays(days int) error {
	if days < 0 {
		return qcli.UsageError{Message: fmt.Sprintf("invalid --days: must be >= 0 (got %d)", days)}
	}
	return nil
}

func writeCASPruneSummary(out io.Writer, result gocas.PruneResult) error {
	total := result.DeletedPriorVersionRecords + result.DeletedSupersededRecords
	_, err := fmt.Fprintf(out, "Deleted CAS records: prior-version=%d superseded=%d total=%d\n",
		result.DeletedPriorVersionRecords,
		result.DeletedSupersededRecords,
		total,
	)
	return err
}
