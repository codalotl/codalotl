package coretools

import (
	"github.com/codalotl/codalotl/internal/llmstream"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func NewToolErrorResult(call llmstream.ToolCall, msg string, srcErr error) llmstream.ToolResult {
	res := llmstream.NewErrorToolResult(msg, call)
	res.SourceErr = srcErr
	return res
}

type WantPathType int

const (
	WantPathTypeAny WantPathType = iota
	WantPathTypeDir
	WantPathTypeFile
)

// NormalizePath accepts a path provided by the LLM, and absSandboxDir (which MUST be an absolute dir), and what the caller wants path to be (anything, a file, or a dir).
// It returns cleaned versions of path (both absolute and relative versions), possibly coerced based on want.
//   - if mustExist and the uncoereced path does not exist, an error is returned.
//   - if !mustExist and the uncoereced path does not exist, no coersion will take place.
//   - if want is Dir but path is a file, path is coereced to a dir (filepath.Dir).
//   - if want is File but path is a dir, an error is returned.
//
// If the path is outside of the sandbox, relativePath is returned as "". The returned absPath and relativePath will never ".." path components. The returned relativePath will
// be "." iff path is the sandbox dir.
func NormalizePath(path string, absSandboxDir string, want WantPathType, mustExist bool) (absPath string, relativePath string, normalizePathErr error) {
	switch want {
	case WantPathTypeAny, WantPathTypeDir, WantPathTypeFile:
	default:
		return "", "", fmt.Errorf("unknown wantPathType: %d", want)
	}

	sandboxClean := filepath.Clean(absSandboxDir)
	if !filepath.IsAbs(sandboxClean) {
		return "", "", fmt.Errorf("sandbox directory must be absolute")
	}

	var resolved string
	if filepath.IsAbs(path) {
		resolved = filepath.Clean(path)
	} else {
		resolved = filepath.Join(sandboxClean, path)
	}

	var fi os.FileInfo
	needStat := mustExist || want != WantPathTypeAny
	if needStat {
		statInfo, statErr := os.Stat(resolved)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				if mustExist {
					return "", "", fmt.Errorf("path does not exist: %w", statErr)
				}
			} else {
				return "", "", statErr
			}
		} else {
			fi = statInfo
		}
	}

	absPath = resolved
	if fi != nil {
		switch want {
		case WantPathTypeDir:
			if !fi.IsDir() {
				absPath = filepath.Dir(resolved)
			}
		case WantPathTypeFile:
			if fi.IsDir() {
				return "", "", fmt.Errorf("path is a directory")
			}
		case WantPathTypeAny:
			// no-op
		}
	}

	rel, relErr := filepath.Rel(sandboxClean, absPath)
	if relErr == nil {
		if !(rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
			relativePath = rel
		}
	}

	return absPath, relativePath, nil
}
