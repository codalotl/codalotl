package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocas/casclarify"
	"github.com/codalotl/codalotl/internal/gocas/casconformance"
	qcas "github.com/codalotl/codalotl/internal/q/cas"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
	toolrefactor "github.com/codalotl/codalotl/internal/tools/refactor"
)

type casRetrieveOutput struct {
	OK             bool `json:"ok"`
	Value          any  `json:"value,omitempty"`
	AdditionalInfo any  `json:"additionalinfo,omitempty"`
}

func validateCASNamespace(namespace string) error {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return fmt.Errorf("missing <namespace>")
	}
	// Namespace must be filesystem-safe because it is used as a directory name.
	if strings.Contains(namespace, "/") || strings.Contains(namespace, "\\") {
		return fmt.Errorf("invalid <namespace>: must not contain path separators")
	}
	return nil
}

func parseCASNamespacesFlag(namespaces string) ([]gocas.NamespaceSpec, error) {
	namespaces = strings.TrimSpace(namespaces)
	if namespaces == "" {
		return nil, fmt.Errorf("missing --namespaces")
	}

	parts := strings.Split(namespaces, ",")
	specs := make([]gocas.NamespaceSpec, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		namespace := strings.TrimSpace(part)
		if namespace == "" {
			return nil, fmt.Errorf("invalid --namespaces: empty namespace")
		}
		if _, ok := seen[namespace]; ok {
			return nil, fmt.Errorf("invalid --namespaces: duplicate namespace %q", namespace)
		}
		spec, err := resolveCASNamespaceSpec(namespace)
		if err != nil {
			return nil, err
		}
		seen[namespace] = struct{}{}
		specs = append(specs, spec)
	}
	return specs, nil
}

func registeredCASNamespaceSpecs() []gocas.NamespaceSpec {
	specs := []gocas.NamespaceSpec{
		casconformance.NamespaceSpec,
		casclarify.NamespaceSpec,
		docsFixCASNamespaceSpec,
	}
	specs = append(specs, toolrefactor.CASNamespaceSpecs()...)
	return specs
}

func resolveCASNamespaceSpec(namespace string) (gocas.NamespaceSpec, error) {
	if err := validateCASNamespace(namespace); err != nil {
		return gocas.NamespaceSpec{}, err
	}
	for _, spec := range registeredCASNamespaceSpecs() {
		if spec.Name == namespace {
			return spec, nil
		}
	}
	return gocas.NamespaceSpec{}, fmt.Errorf("unknown CAS namespace %q", namespace)
}

func sortedCASNamespaceSpecs() []gocas.NamespaceSpec {
	specs := registeredCASNamespaceSpecs()
	sort.Slice(specs, func(i, j int) bool {
		if specs[i].Name == specs[j].Name {
			return specs[i].Version < specs[j].Version
		}
		return specs[i].Name < specs[j].Name
	})
	return specs
}

func casDBForBaseDir(baseDir string) (*gocas.DB, error) {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return nil, fmt.Errorf("cas: missing base dir")
	}
	db, err := gocas.NewDBForBaseDir(baseDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(db.DB.AbsRoot, 0755); err != nil {
		return nil, fmt.Errorf("cas: create db root %q: %w", db.DB.AbsRoot, err)
	}
	return db, nil
}

func casReadDBForBaseDir(baseDir string) (*gocas.DB, error) {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return nil, fmt.Errorf("cas: missing base dir")
	}
	return gocas.NewDBForBaseDir(baseDir)
}

func casQDBForBaseDir(baseDir string) (*qcas.DB, error) {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return nil, fmt.Errorf("cas: missing base dir")
	}
	absRoot, err := gocas.RootDirForBaseDir(baseDir)
	if err != nil {
		return nil, err
	}
	// Unlike `cas set`, TUI only needs read access; avoid creating directories as
	// a side effect of launching the UI.
	return &qcas.DB{AbsRoot: absRoot}, nil
}

func runCASRecertify(out io.Writer, packagePath string, namespaces string) error {
	specs, err := parseCASNamespacesFlag(namespaces)
	if err != nil {
		return qcli.UsageError{Message: err.Error()}
	}

	pkg, mod, err := loadPackageArg(packagePath)
	if err != nil {
		return err
	}
	db, err := casDBForBaseDir(mod.AbsolutePath)
	if err != nil {
		return err
	}

	var missingPrior bool
	for _, spec := range specs {
		result, err := db.RecertifyPackage(pkg, spec)
		if err != nil {
			return err
		}
		if err := writeCASRecertifyResult(out, spec.Name, result); err != nil {
			return err
		}
		if result.Status == gocas.PackageRecertificationStatusNoPrior {
			missingPrior = true
		}
	}
	if missingPrior {
		return qcli.ExitError{Code: 1, Err: errors.New("")}
	}
	return nil
}

func writeCASRecertifyResult(out io.Writer, namespace string, result gocas.PackageRecertificationResult) error {
	switch result.Status {
	case gocas.PackageRecertificationStatusCurrent:
		if _, err := fmt.Fprintf(out, "%s: current (%s)\n", namespace, shortCASHex(result.CurrentHash)); err != nil {
			return err
		}
	case gocas.PackageRecertificationStatusRecertified:
		if _, err := fmt.Fprintf(out, "%s: recertified (%s -> %s)\n", namespace, shortCASHex(result.SourceHash), shortCASHex(result.CurrentHash)); err != nil {
			return err
		}
	case gocas.PackageRecertificationStatusNoPrior:
		if _, err := fmt.Fprintf(out, "%s: no prior record\n", namespace); err != nil {
			return err
		}
	default:
		if _, err := fmt.Fprintf(out, "%s: %s\n", namespace, result.Status); err != nil {
			return err
		}
	}
	for _, warning := range result.Warnings {
		if _, err := fmt.Fprintf(out, "  warning: %s\n", warning); err != nil {
			return err
		}
	}
	return nil
}

func shortCASHex(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}
