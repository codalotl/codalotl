package gocodecontext

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ModuleInfo returns information about the current Go module. It identifies the go.mod file
// by starting at absDir and walking up until it finds a go.mod file.
//
// The returned string is intended as LLM context and is intentionally opaque to callers.
//
// NOTES:
//   - For now, this just returns the go.mod file itself.
func ModuleInfo(absDir string) (string, error) {
	dir := filepath.Clean(absDir)
	if !filepath.IsAbs(dir) {
		if resolved, absErr := filepath.Abs(dir); absErr == nil {
			dir = resolved
		}
	}

	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		dir = filepath.Dir(dir)
	}

	root, err := findNearestGoModDir(dir)
	if err != nil {
		return "", err
	}

	modBytes, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}

	return strings.TrimSpace(string(modBytes)), nil
}
