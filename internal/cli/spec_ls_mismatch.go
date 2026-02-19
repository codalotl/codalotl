package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
	"github.com/codalotl/codalotl/internal/specmd"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func runSpecLsMismatch(ctx context.Context, out io.Writer, pattern string) error {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return qcli.UsageError{Message: "missing <pkg/pattern>"}
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	mod, err := gocode.NewModule(wd)
	if err != nil {
		return err
	}
	pkgDirs, err := goListPackageDirs(ctx, pattern)
	if err != nil {
		return err
	}
	mismatches := map[string]struct{}{}
	for _, absPkgDir := range pkgDirs {
		specPath := filepath.Join(absPkgDir, "SPEC.md")
		if _, err := os.Stat(specPath); err != nil {
			continue
		}
		spec, err := specmd.Read(specPath)
		if err != nil {
			continue
		}
		diffs, err := spec.ImplementationDiffs()
		if err != nil {
			continue
		}
		if len(diffs) == 0 {
			continue
		}
		display, ok := displayPackagePath(mod.AbsolutePath, absPkgDir)
		if !ok {
			continue
		}
		mismatches[display] = struct{}{}
	}
	lines := make([]string, 0, len(mismatches))
	for line := range mismatches {
		lines = append(lines, line)
	}
	sort.Strings(lines)
	for _, line := range lines {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	return nil
}
func displayPackagePath(moduleAbsDir, packageAbsDir string) (string, bool) {
	rel, err := filepath.Rel(moduleAbsDir, packageAbsDir)
	if err != nil {
		return "", false
	}
	if rel == "." {
		return ".", true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return "./" + filepath.ToSlash(rel), true
}
func goListPackageDirs(ctx context.Context, pattern string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-e", "-f", "{{.Dir}}", pattern)
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
func parseNonEmptyLines(b []byte) []string {
	var out []string
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
