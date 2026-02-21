package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
)

func runCASLsUnset(ctx context.Context, out io.Writer, namespace string) error {
	if err := validateCASNamespace(namespace); err != nil {
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

	db, err := casDBForBaseDir(mod.AbsolutePath)
	if err != nil {
		return err
	}

	var missing []string
	for _, absPkgDir := range pkgDirs {
		display, ok := displayPackagePath(mod.AbsolutePath, absPkgDir)
		if !ok {
			continue
		}

		rel, err := filepath.Rel(mod.AbsolutePath, absPkgDir)
		if err != nil {
			continue
		}

		pkg, err := mod.LoadPackageByRelativeDir(rel)
		if err != nil {
			continue
		}

		var raw json.RawMessage
		ok, _, err = db.RetrieveOnPackage(pkg, gocas.Namespace(namespace), &raw)
		if err != nil {
			continue
		}
		if !ok {
			missing = append(missing, display)
		}
	}

	sort.Strings(missing)
	for _, line := range missing {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	return nil
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
