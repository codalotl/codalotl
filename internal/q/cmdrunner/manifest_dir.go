package cmdrunner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var defaultManifestFilenames = map[string][]string{
	"c":     {"Makefile"},
	"cpp":   {"Makefile"},
	"cs":    {"Directory.Build.props", "Directory.Packages.props", "NuGet.Config"},
	"go":    {"go.mod"},
	"hs":    {"package.yaml", "stack.yaml"},
	"java":  {"pom.xml", "build.gradle", "build.gradle.kts"},
	"js":    {"package.json"},
	"kt":    {"build.gradle.kts", "build.gradle", "pom.xml"},
	"php":   {"composer.json"},
	"py":    {"pyproject.toml", "requirements.txt", "setup.cfg"},
	"rb":    {"Gemfile"},
	"rs":    {"Cargo.toml"},
	"scala": {"build.sbt", "pom.xml"},
	"swift": {"Package.swift"},
	"ts":    {"package.json"},
}

type manifestDirResolver struct {
	rootDir      string
	langOverride string
	manifestMap  map[string][]string
}

func newManifestDirResolver(rootDir string, inputs map[string]any) *manifestDirResolver {
	var lang string
	if inputs != nil {
		if raw, ok := inputs["Lang"]; ok {
			if s, ok := raw.(string); ok {
				lang = strings.ToLower(strings.TrimSpace(s))
			}
		}
	}

	return &manifestDirResolver{
		rootDir:      rootDir,
		langOverride: lang,
		manifestMap:  defaultManifestFilenames,
	}
}

func (r *manifestDirResolver) manifestDir(path string) (string, error) {
	startDir := r.startDir(path)
	lang := r.langOverride
	if lang == "" {
		lang = r.detectLanguage(startDir)
	}
	if lang == "" {
		return r.rootDir, nil
	}

	manifests, ok := r.manifestMap[lang]
	if !ok || len(manifests) == 0 {
		return r.rootDir, nil
	}

	current := startDir
	for {
		for _, manifest := range manifests {
			if fileExists(filepath.Join(current, manifest)) {
				return current, nil
			}
		}

		if r.isRoot(current) {
			break
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return r.rootDir, nil
}

func (r *manifestDirResolver) startDir(p string) string {
	if p == "" {
		return r.rootDir
	}

	candidate := p

	if info, err := os.Stat(candidate); err == nil {
		if !info.IsDir() {
			candidate = filepath.Dir(candidate)
		}
	} else {
		candidate = filepath.Dir(candidate)
		for {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				break
			}

			parent := filepath.Dir(candidate)
			if parent == candidate {
				break
			}
			candidate = parent
		}
	}

	return filepath.Clean(candidate)
}

func (r *manifestDirResolver) detectLanguage(startDir string) string {
	current := startDir
	for {
		if lang := detectLanguageInDir(current, r.manifestMap); lang != "" {
			return lang
		}

		if r.isRoot(current) {
			break
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return ""
}

func (r *manifestDirResolver) isRoot(path string) bool {
	if r.rootDir == "" {
		return false
	}
	return samePath(r.rootDir, path)
}

func detectLanguageInDir(dir string, manifestMap map[string][]string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(entry.Name())), ".")
		if ext == "" {
			continue
		}

		if _, ok := manifestMap[ext]; ok {
			return ext
		}
	}

	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func samePath(a, b string) bool {
	cleanA := filepath.Clean(a)
	cleanB := filepath.Clean(b)

	if runtime.GOOS == "windows" {
		return strings.EqualFold(cleanA, cleanB)
	}

	return cleanA == cleanB
}

// ManifestDir returns the manifest dir for `path` and the path relative to that manifest dir.
//
// Conceptually equivalent to:
//   - {{ manifestDir .path }}
//   - {{ relativeTo .path (manifestDir .path) }}
//
// Errors are returned if `rootDir` is empty/invalid/not a directory, or if the relative path computation fails.
func ManifestDir(rootDir string, path string) (string, string, error) {
	if rootDir == "" {
		return "", "", errors.New("cmdrunner: rootDir must not be empty")
	}

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", "", fmt.Errorf("cmdrunner: resolve rootDir: %w", err)
	}

	rootInfo, err := os.Stat(absRoot)
	if err != nil {
		return "", "", fmt.Errorf("cmdrunner: rootDir: %w", err)
	}
	if !rootInfo.IsDir() {
		return "", "", fmt.Errorf("cmdrunner: rootDir %q is not a directory", absRoot)
	}

	resolvedPath := absRoot
	if path != "" {
		if filepath.IsAbs(path) {
			resolvedPath = filepath.Clean(path)
		} else {
			resolvedPath = filepath.Join(absRoot, path)
		}
	}

	resolver := newManifestDirResolver(absRoot, nil)
	manifestDir, err := resolver.manifestDir(resolvedPath)
	if err != nil {
		return "", "", err
	}

	relativePath, err := filepath.Rel(manifestDir, resolvedPath)
	if err != nil {
		return "", "", err
	}

	return manifestDir, relativePath, nil
}
