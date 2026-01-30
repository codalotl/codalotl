package cmdrunner

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

type templateHelperProvider struct {
	rootDir string
	inputs  map[string]any
}

func newTemplateHelperProvider(rootDir string, inputs map[string]any) *templateHelperProvider {
	return &templateHelperProvider{
		rootDir: rootDir,
		inputs:  inputs,
	}
}

func (p *templateHelperProvider) funcMap() template.FuncMap {
	return template.FuncMap{
		"manifestDir": p.manifestDir,
		"relativeTo":  p.relativeTo,
		"repoDir":     p.repoDir,
	}
}

func (p *templateHelperProvider) manifestDir(path string) (string, error) {
	resolver := newManifestDirResolver(p.rootDir, p.inputs)
	resolvedPath := p.resolvePath(path)

	dir, err := resolver.manifestDir(resolvedPath)
	if err != nil {
		return "", err
	}
	return dir, nil
}

func (p *templateHelperProvider) relativeTo(path, base string) (string, error) {
	if base == "" {
		return "", fmt.Errorf("relativeTo: base path is empty")
	}

	resolvedBase := p.resolvePath(base)
	resolvedPath := p.resolvePath(path)

	relative, err := filepath.Rel(resolvedBase, resolvedPath)
	if err != nil {
		return "", err
	}

	return relative, nil
}

func (p *templateHelperProvider) repoDir(path string) (string, error) {
	resolver := newManifestDirResolver(p.rootDir, p.inputs)
	startDir := resolver.startDir(p.resolvePath(path))

	current := startDir
	for {
		if hasGitDir(current) {
			return current, nil
		}

		if resolver.isRoot(current) {
			return current, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return p.rootDir, nil
		}
		current = parent
	}
}

func (p *templateHelperProvider) resolvePath(path string) string {
	if path == "" {
		return p.rootDir
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(p.rootDir, path)
}

func hasGitDir(dir string) bool {
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return false
	}
	return info.IsDir() || info.Mode().IsRegular()
}
