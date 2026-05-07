package docubot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/specmd"
)

func specContextForPackage(pkg *gocode.Package, contextModule *gocode.Module) (string, error) {
	if contextModule == nil {
		contextModule = pkg.Module
	}
	if contextModule == nil {
		return "", fmt.Errorf("package module is nil")
	}

	specPath := filepath.Join(contextModule.AbsolutePath, pkg.RelativeDir, "SPEC.md")
	spec, err := specmd.Read(specPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}

	stripped, err := spec.WithoutPublicAPI()
	if err != nil {
		return "", err
	}

	body := strings.TrimSpace(stripped.Body)
	if body == "" {
		return "", nil
	}

	return "Target package SPEC.md (Public API sections removed):\n\n````markdown\n" + body + "\n````\n\n", nil
}
