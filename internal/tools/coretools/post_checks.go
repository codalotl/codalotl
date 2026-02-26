package coretools

import (
	"context"
	"path/filepath"
	"strings"
)

type ToolPostChecks struct {
	RunDiagnostics func(ctx context.Context, sandboxDir string, targetDir string) (string, error)
	FixLints       func(ctx context.Context, sandboxDir string, targetDir string) (string, error)
}

type ApplyPatchPostChecks = ToolPostChecks
type EditPostChecks = ToolPostChecks
type WritePostChecks = ToolPostChecks

func shouldRunPostChecks(postChecks *ToolPostChecks) bool {
	return postChecks != nil && (postChecks.RunDiagnostics != nil || postChecks.FixLints != nil)
}

func runPostChecks(ctx context.Context, sandboxAbsDir string, postChecks *ToolPostChecks, changedPaths []string) ([]string, error) {
	if len(changedPaths) == 0 || !shouldRunPostChecks(postChecks) {
		return nil, nil
	}

	dirSet := make(map[string]struct{})
	for _, changedPath := range changedPaths {
		abs := changedPath
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(sandboxAbsDir, filepath.FromSlash(changedPath))
		}
		if !isPathWithinSandbox(sandboxAbsDir, abs) {
			continue
		}
		dir := filepath.Dir(abs)
		if dir == "" || dir == "." {
			dir = sandboxAbsDir
		}
		dir = filepath.Clean(dir)
		if !isPathWithinSandbox(sandboxAbsDir, dir) {
			continue
		}
		dirSet[dir] = struct{}{}
	}

	if len(dirSet) == 0 {
		return nil, nil
	}

	if len(dirSet) > 1 {
		// TODO: Support diagnostics and lint checks when changes span multiple directories.
		return nil, nil
	}

	var targetDir string
	for dir := range dirSet {
		targetDir = dir
	}

	var outputs []string
	if postChecks.RunDiagnostics != nil {
		diagnosticsOutput, err := postChecks.RunDiagnostics(ctx, sandboxAbsDir, targetDir)
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, diagnosticsOutput)
	}

	if postChecks.FixLints != nil {
		lintOutput, err := postChecks.FixLints(ctx, sandboxAbsDir, targetDir)
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, lintOutput)
	}

	return outputs, nil
}

func isPathWithinSandbox(sandboxAbsDir string, path string) bool {
	if !filepath.IsAbs(path) {
		path = filepath.Join(sandboxAbsDir, path)
	}
	rel, err := filepath.Rel(sandboxAbsDir, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." {
		return false
	}
	prefix := ".." + string(filepath.Separator)
	if strings.HasPrefix(rel, prefix) {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}
