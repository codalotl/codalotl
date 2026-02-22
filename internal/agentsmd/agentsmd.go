// Package agentsmd implements support for reading AGENTS.md files.
package agentsmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const agentsFilename = "AGENTS.md"

// Read will read AGENTS.md files in cwd, its parent, up to sandboxDir. It returns a concatenation of all AGENTS.md files it finds, in a format that can be directly
// supplied to an LLM. This returned concatenated string may have some explanation and metadata (ex: filenames). If there are no AGENTS.md files (or they are empty/whitespace-only),
// Read returns ("", nil).
//
// cwd must be in sandboxDir. They may be the same path.
//
// Example return value:
//
//	The following AGENTS.md files were found, and may provide relevant instructions. The nearest AGENTS.md file to the target code takes precedence.
//
//	AGENTS.md found at /home/user/proj/AGENTS.md:
//	<file text>
//
//	AGENTS.md found at /home/user/proj/subdir/AGENTS.md:
//	<file text>
func Read(sandboxDir, cwd string) (string, error) {
	sandboxAbs, err := absClean(sandboxDir)
	if err != nil {
		return "", fmt.Errorf("abs sandboxDir: %w", err)
	}
	cwdAbs, err := absClean(cwd)
	if err != nil {
		return "", fmt.Errorf("abs cwd: %w", err)
	}

	rel, err := filepath.Rel(sandboxAbs, cwdAbs)
	if err != nil {
		return "", fmt.Errorf("rel(cwd, sandboxDir): %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("cwd must be within sandboxDir: cwd=%q sandboxDir=%q", cwdAbs, sandboxAbs)
	}

	type foundFile struct {
		path string
		text string
	}

	var found []foundFile
	for dir := cwdAbs; ; dir = filepath.Dir(dir) {
		p := filepath.Join(dir, agentsFilename)
		b, err := os.ReadFile(p)
		if err == nil {
			txt := strings.TrimSpace(string(b))
			if txt != "" {
				found = append(found, foundFile{path: p, text: txt})
			}
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("read %s: %w", p, err)
		}

		if dir == sandboxAbs {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Should be impossible if sandboxAbs is an ancestor of cwdAbs, but guard anyway.
			break
		}
	}

	if len(found) == 0 {
		return "", nil
	}

	// We walked from cwd -> sandbox. Emit from sandbox -> cwd so that "nearer"
	// files (closer to cwd) appear later and therefore take precedence.
	for i, j := 0, len(found)-1; i < j; i, j = i+1, j-1 {
		found[i], found[j] = found[j], found[i]
	}

	var out bytes.Buffer
	out.WriteString("The following AGENTS.md files were found, and may provide relevant instructions. The nearest AGENTS.md file to the target code takes precedence.\n")
	out.WriteString("\n")

	for i, f := range found {
		if i != 0 {
			out.WriteString("\n\n")
		}
		out.WriteString("AGENTS.md found at ")
		out.WriteString(f.path)
		out.WriteString(":\n")
		out.WriteString(f.text)
		out.WriteString("\n")
	}

	return out.String(), nil
}

func absClean(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("path is empty")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}
