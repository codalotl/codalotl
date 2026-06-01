package coretools

import (
	"context"
	"path/filepath"
	"strings"
)

// ToolPostChecks configures optional post-change hooks for file-mutating tools.
//
// Hooks run only for changes that resolve to one directory inside the sandbox. When both hooks are set, RunDiagnostics runs before FixLints.
type ToolPostChecks struct {
	// RunDiagnostics checks the changed target directory and returns text to append to the tool result.
	//
	// sandboxDir is the sandbox root, targetDir is the directory containing the changed file, and a non-nil error is reported as a post-check failure.
	RunDiagnostics func(ctx context.Context, sandboxDir string, targetDir string) (string, error)

	// FixLints runs lint fixes for the changed target directory and returns text to append to the tool result.
	//
	// sandboxDir is the sandbox root, targetDir is the directory containing the changed file, and a non-nil error is reported as a post-check failure.
	FixLints func(ctx context.Context, sandboxDir string, targetDir string) (string, error)
}

// ApplyPatchPostChecks is an alias for ToolPostChecks that names optional post-change hooks for the apply_patch tool.
type ApplyPatchPostChecks = ToolPostChecks

// EditPostChecks is an alias for ToolPostChecks that names optional post-change hooks for the edit tool.
type EditPostChecks = ToolPostChecks

// WritePostChecks is an alias for ToolPostChecks that names optional post-change hooks for the write tool.
type WritePostChecks = ToolPostChecks

func shouldRunPostChecks(postChecks *ToolPostChecks) bool {
	return postChecks != nil && (postChecks.RunDiagnostics != nil || postChecks.FixLints != nil)
}

// The runPostChecks function runs configured post-change hooks for changed files in a single sandbox directory. It ignores changed paths outside the sandbox and
// treats relative paths as sandbox-relative. If no hooks are configured, no eligible paths remain, or eligible paths span multiple directories, it returns no output
// and no error. RunDiagnostics runs before FixLints, and the first hook error stops processing and is returned.
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

// The isPathWithinSandbox function reports whether path is the sandbox root or lexically relative to it without a leading "..". Relative paths are interpreted from
// sandboxAbsDir, and the check is lexical rather than symlink-aware.
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
