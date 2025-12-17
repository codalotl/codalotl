package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codalotl/codalotl/internal/gocode"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
)

func loadPackageArg(arg string) (*gocode.Package, *gocode.Module, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return nil, nil, qcli.UsageError{Message: "missing <path/to/pkg>"}
	}
	if strings.Contains(arg, "...") {
		return nil, nil, qcli.UsageError{Message: `package patterns ("...") are not supported; provide a single package directory`}
	}

	// Prefer interpreting arg as a filesystem directory (per spec examples), but
	// fall back to treating it as an import path.
	if absDir, ok, err := resolveExistingDir(arg); err != nil {
		return nil, nil, err
	} else if ok {
		mod, err := gocode.NewModule(absDir)
		if err != nil {
			return nil, nil, err
		}

		rel, err := filepath.Rel(mod.AbsolutePath, absDir)
		if err != nil {
			return nil, nil, err
		}
		rel = filepath.Clean(rel)
		pkg, err := mod.LoadPackageByRelativeDir(rel)
		if err != nil {
			return nil, nil, err
		}
		return pkg, mod, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, fmt.Errorf("get working directory: %w", err)
	}
	mod, err := gocode.NewModule(cwd)
	if err != nil {
		return nil, nil, err
	}
	pkg, err := mod.LoadPackageByImportPath(strings.TrimSuffix(arg, "/"))
	if err != nil {
		if errors.Is(err, gocode.ErrImportNotInModule) {
			return nil, nil, qcli.UsageError{Message: fmt.Sprintf("import path %q is not in module %q", arg, mod.Name)}
		}
		return nil, nil, err
	}
	return pkg, mod, nil
}

func resolveExistingDir(pathArg string) (absDir string, ok bool, _ error) {
	pathArg = strings.TrimSpace(pathArg)
	if pathArg == "" {
		return "", false, nil
	}

	// filepath.Clean handles optional trailing separators.
	clean := filepath.Clean(pathArg)
	cwd, err := os.Getwd()
	if err != nil {
		return "", false, fmt.Errorf("get working directory: %w", err)
	}

	if !filepath.IsAbs(clean) {
		clean = filepath.Join(cwd, clean)
	}

	info, err := os.Stat(clean)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if !info.IsDir() {
		return "", false, nil
	}
	return clean, true, nil
}
