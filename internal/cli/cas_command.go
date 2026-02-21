package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codalotl/codalotl/internal/gocas"
	qcas "github.com/codalotl/codalotl/internal/q/cas"
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
func casDBForBaseDir(baseDir string) (*gocas.DB, error) {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return nil, fmt.Errorf("cas: missing base dir")
	}
	if !filepath.IsAbs(baseDir) {
		abs, err := filepath.Abs(baseDir)
		if err != nil {
			return nil, fmt.Errorf("cas: resolve base dir: %w", err)
		}
		baseDir = abs
	}
	absRoot, err := casDBRootDir(baseDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absRoot, 0755); err != nil {
		return nil, fmt.Errorf("cas: create db root %q: %w", absRoot, err)
	}
	return &gocas.DB{
		BaseDir: baseDir,
		DB: qcas.DB{
			AbsRoot: absRoot,
		},
	}, nil
}
func casDBRootDir(baseDir string) (string, error) {
	if v := strings.TrimSpace(os.Getenv("CODALOTL_CAS_DB")); v != "" {
		if filepath.IsAbs(v) {
			return v, nil
		}
		abs, err := filepath.Abs(v)
		if err != nil {
			return "", fmt.Errorf("cas: resolve CODALOTL_CAS_DB: %w", err)
		}
		return abs, nil
	}
	gitDir, err := nearestGitDir(baseDir)
	if err != nil {
		return "", err
	}
	if gitDir != "" {
		return filepath.Join(gitDir, ".codalotl", "cas"), nil
	}
	return baseDir, nil
}
func nearestGitDir(start string) (string, error) {
	start = strings.TrimSpace(start)
	if start == "" {
		return "", nil
	}
	if !filepath.IsAbs(start) {
		abs, err := filepath.Abs(start)
		if err != nil {
			return "", err
		}
		start = abs
	}
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}
