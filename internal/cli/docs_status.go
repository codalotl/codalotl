package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codalotl/codalotl/internal/docubot"
	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
	"github.com/codalotl/codalotl/internal/q/remotemonitor"
	"github.com/codalotl/codalotl/internal/updatedocs"
)

const (
	docsStatusCurrent = "current"
	docsStatusNeeded  = "needed"
	docsStatusError   = "error"
)

var runDocubotNeedsDocs = docubot.NeedsDocs

// A docsStatusRow contains the documentation status for one package.
type docsStatusRow struct {
	Package string // Display package path.
	DocsAdd string // Status of missing-documentation coverage.
	DocsFix string // Status of material documentation correctness.
	Reflow  string // Status of deterministic documentation reflow.
}

func newDocsStatusCommand(runWithConfig runWithConfigFunc) *qcli.Command {
	statusCmd := &qcli.Command{
		Name:             "status",
		Short:            "Print per-package documentation status across discovered repo modules.",
		Long:             "Prints a table for packages across Go modules discovered from the nearest git repo, showing docs-add, docs-fix, and documentation reflow status.",
		Args:             qcli.NoArgs,
		NoPositionalArgs: true,
		Example: strings.TrimSpace(`
codalotl docs status
`),
		Run: runWithConfig("docs_status", func(c *qcli.Context, cfg Config, _ *remotemonitor.Monitor) error {
			return runDocsStatus(c.Context, c.Out, cfg.ReflowWidth)
		}),
	}
	return statusCmd
}

// runDocsStatus writes the per-package documentation status table for packages discovered under the nearest Git repository. It is read-only; package-specific failures
// are reported as error statuses in their rows.
func runDocsStatus(ctx context.Context, out io.Writer, reflowWidth int) error {
	repoRoot, pkgDirs, err := goListPackageDirsUnderNearestGitRepo(ctx)
	if err != nil {
		return err
	}

	rows := make([]docsStatusRow, 0, len(pkgDirs))
	dbs := map[string]*gocas.DB{}
	for _, pkgDir := range pkgDirs {
		display, ok := displayPackagePath(repoRoot, pkgDir.absDir)
		if !ok {
			continue
		}

		row := docsStatusRow{
			Package: display,
			DocsAdd: docsStatusError,
			DocsFix: docsStatusError,
			Reflow:  docsStatusError,
		}

		pkg, err := loadPackageFromRepoDir(pkgDir)
		if err != nil {
			rows = append(rows, row)
			continue
		}

		row.DocsAdd = docsAddStatus(pkg)
		row.DocsFix = docsFixStatus(pkgDir.mod.AbsolutePath, pkg, dbs)
		row.Reflow = docsReflowStatus(pkg, reflowWidth)
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Package < rows[j].Package
	})

	return writeDocsStatusTable(out, rows)
}

func loadPackageFromRepoDir(pkgDir casRepoPackageDir) (*gocode.Package, error) {
	relDir, err := filepath.Rel(pkgDir.mod.AbsolutePath, pkgDir.absDir)
	if err != nil {
		return nil, err
	}
	if relDir == ".." || strings.HasPrefix(relDir, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("package %q is outside module %q", pkgDir.absDir, pkgDir.mod.AbsolutePath)
	}
	return pkgDir.mod.LoadPackageByRelativeDir(relDir)
}

func docsAddStatus(pkg *gocode.Package) string {
	needsDocs, err := runDocubotNeedsDocs(pkg, docubot.AddDocsOptions{
		OnlyDocumentImportantIdentifiers: true,
	})
	if err != nil {
		return docsStatusError
	}
	if needsDocs {
		return docsStatusNeeded
	}
	return docsStatusCurrent
}

// docsFixStatus reports the docs-fix status for pkg from the module CAS database. It reuses and populates dbs by module root. It returns docsStatusCurrent only
// for a whole-package docs-fix record for the package's current contents; identifier-limited records count as docsStatusNeeded, and database errors return docsStatusError.
func docsFixStatus(moduleRoot string, pkg *gocode.Package, dbs map[string]*gocas.DB) string {
	db, err := cachedCASReadDBForBaseDir(dbs, moduleRoot)
	if err != nil {
		return docsStatusError
	}

	var value docsFixCASValue
	ok, _, err := db.Retrieve(pkg, docsFixCASNamespaceSpec, &value)
	if err != nil {
		return docsStatusError
	}
	if ok && value.Mode == docsFixModeWholePackage {
		return docsStatusCurrent
	}
	return docsStatusNeeded
}

func docsReflowStatus(pkg *gocode.Package, reflowWidth int) string {
	checkPkg, err := pkg.Clone()
	if err != nil {
		return docsStatusError
	}
	defer checkPkg.Module.DeleteClone()

	modified, skipped, err := updatedocs.ReflowDocumentationPaths([]string{checkPkg.AbsolutePath()}, true, updatedocs.Options{
		ReflowMaxWidth: reflowWidth,
	})
	if err != nil || len(skipped) > 0 {
		return docsStatusError
	}
	if len(modified) > 0 {
		return docsStatusNeeded
	}
	return docsStatusCurrent
}

// writeDocsStatusTable writes rows as an aligned docs status table.
func writeDocsStatusTable(w io.Writer, rows []docsStatusRow) error {
	tableRows := make([][]string, 0, len(rows))
	for _, r := range rows {
		tableRows = append(tableRows, []string{r.Package, r.DocsAdd, r.DocsFix, r.Reflow})
	}
	return writeAlignedTable(w, []string{"package", "docs_add", "docs_fix", "reflow"}, tableRows)
}
