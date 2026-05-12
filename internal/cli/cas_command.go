package cli

import (
	"fmt"
	"os"
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
