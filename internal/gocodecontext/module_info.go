package gocodecontext

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ModuleInfo returns information about the current Go module. It identifies the go.mod file by starting at absDir and walking up until it finds a go.mod file.
//
// It returns an LLM context string that can be directly dropped into an LLM, and an error, if any.
//
// The LLM context string is intentionally opaque (callers should not rely on parsing it; they should directly drop it into an LLM). That said, conceptually, it
// might look like:
//
//	module github.com/someuser/myproj
//
//	go 1.24
//
// NOTES:
//   - For now, this just returns the go.mod file itself.
//   - Need to do more research about how big these can get. We may want to implement things like limiting deps to direct dependencies; stripping comments; being
//     more concise; module search.
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
