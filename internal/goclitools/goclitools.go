// goclitools exposes methods that abstract over common Go CLI tools like gopls, goimports, gofmt, etc.
package goclitools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var (
	muToolAvailability     sync.RWMutex
	testedToolAvailability = false
	goimportsAvail         = false
	goplsAvail             = false
	gofmtAvail             = false
	// in the future, we may want goplsServerAvail
)

// Ref is a parsed representation of something like this:
//
//	/abs/path/to/file.go:247:37-43
//
// ColumnStart and ColumnEnd are 1-based byte offsets in the line, and ColumnEnd is the first byte past the reference.
type Ref struct {
	AbsPath     string
	Line        int
	ColumnStart int
	ColumnEnd   int
}

// Gofmt runs `gofmt` on filenameOrDir with -w -l. It returns true if anything was formatted.
func Gofmt(filenameOrDir string) (bool, error) {
	discoverTools()

	if !gofmtAvail {
		return false, fmt.Errorf("gofmt not available: install gofmt (part of Go toolchain)")
	}
	// Use `gofmt -w -l` directly on a file or directory.
	// Detect changes by checking if -l output lists any files.
	if _, err := os.Stat(filenameOrDir); err != nil {
		return false, err
	}

	cmd := exec.Command("gofmt", "-w", "-l", filenameOrDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("gofmt failed: %v: %s", err, string(out))
	}

	changedList := strings.TrimSpace(string(out))
	return changedList != "", nil
}

// FixImports fixes imports for a single file or for all .go files in a directory (non-recursive), updating files in place. It returns true if any file was changed.
//
// It prefers goimports. When given a directory, goimports is run once on the directory's .go files. If goimports is unavailable, it falls back to gopls; since gopls
// does not accept a directory for import organization, each .go file is processed individually with `gopls imports -w`.
//
// An error is returned if no tools are available or if a tool returns an error.
func FixImports(filenameOrDir string) (bool, error) {
	discoverTools()

	fi, err := os.Stat(filenameOrDir)
	if err != nil {
		return false, err
	}

	useGoimports := goimportsAvail
	useGopls := goplsAvail

	if !useGoimports && !useGopls {
		return false, fmt.Errorf("no tools available: install goimports or gopls")
	}

	// Helper to process a single file
	runOnFile := func(path string) (bool, error) {
		before, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}

		var out []byte
		if useGoimports {
			cmd := exec.Command("goimports", "-w", path)
			out, err = cmd.CombinedOutput()
			if err != nil {
				return false, fmt.Errorf("goimports failed: %v: %s", err, string(out))
			}
		} else {
			cmd := exec.Command("gopls", "imports", "-w", path)
			out, err = cmd.CombinedOutput()
			if err != nil {
				return false, fmt.Errorf("gopls imports failed: %v: %s", err, string(out))
			}
		}

		after, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}

		return string(before) != string(after), nil
	}

	if !fi.IsDir() {
		return runOnFile(filenameOrDir)
	}

	// Directory handling (non-recursive): list *.go files and process
	entries, err := os.ReadDir(filenameOrDir)
	if err != nil {
		return false, err
	}
	goFiles := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".go" {
			continue
		}
		goFiles = append(goFiles, filepath.Join(filenameOrDir, e.Name()))
	}

	if len(goFiles) == 0 {
		return false, nil
	}

	// Capture initial contents for change detection
	beforeByPath := make(map[string]string, len(goFiles))
	for _, p := range goFiles {
		b, err := os.ReadFile(p)
		if err != nil {
			return false, err
		}
		beforeByPath[p] = string(b)
	}

	if useGoimports {
		// NOTE: goimports takes a directory, but it's recursive (and if .git exists and has any branches ending with .go, it will traverse into .git and fail.)
		// NOTE2: globs like *.go are handled by shell, not goimports. So just pass files explicitly...
		args := []string{"-w"}
		args = append(args, goFiles...)
		cmd := exec.Command("goimports", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return false, fmt.Errorf("goimports failed: %v: %s", err, string(out))
		}
	} else {
		for _, p := range goFiles {
			cmd := exec.Command("gopls", "imports", "-w", p)
			if out, err := cmd.CombinedOutput(); err != nil {
				return false, fmt.Errorf("gopls imports failed on %s: %v: %s", p, err, string(out))
			}
		}
	}

	anyChanged := false
	for _, p := range goFiles {
		after, err := os.ReadFile(p)
		if err != nil {
			return false, err
		}
		if beforeByPath[p] != string(after) {
			anyChanged = true
		}
	}

	return anyChanged, nil
}

// Rename renames the identifier at (line, column) in the given file to newName using gopls. It writes changes in-place. If gopls reports shadowing or other semantic
// issues, an error is returned.
func Rename(filePath string, line, column int, newName string) error {
	discoverTools()

	if !goplsAvail {
		return fmt.Errorf("gopls not available: install gopls")
	}

	if newName == "" {
		return fmt.Errorf("new name must be non-empty")
	}

	abs, err := filepath.Abs(filePath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return err
	}

	pos := fmt.Sprintf("%s:%d:%d", abs, line, column)
	cmd := exec.Command("gopls", "rename", "-w", pos, newName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gopls rename failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// References calls `gopls references` and returns references to the identifier at line and column (1-based). Column is measured in utf-8 bytes (not unicode runes).
func References(filePath string, line, column int) ([]Ref, error) {
	discoverTools()

	if !goplsAvail {
		return nil, fmt.Errorf("gopls not available: install gopls")
	}
	if filePath == "" {
		return nil, fmt.Errorf("filePath must be non-empty")
	}
	if line <= 0 || column <= 0 {
		return nil, fmt.Errorf("line and column must be >= 1")
	}

	abs, err := filepath.Abs(filePath)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, err
	}

	pos := fmt.Sprintf("%s:%d:%d", abs, line, column)
	cmd := exec.Command("gopls", "references", pos)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gopls references failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	refs := parseReferencesOutput(string(out))
	return refs, nil
}

// sets testedToolAvailability xyzAvail, protected by muToolAvailability.
func discoverTools() {
	muToolAvailability.Lock()
	defer muToolAvailability.Unlock()

	if testedToolAvailability {
		return
	}

	// Probe for goimports, gopls, and gofmt availability.
	if lp, err := exec.LookPath("goimports"); err == nil && lp != "" {
		goimportsAvail = true
	} else {
		goimportsAvail = false
	}

	if lp, err := exec.LookPath("gopls"); err == nil && lp != "" {
		goplsAvail = true
	} else {
		goplsAvail = false
	}

	if lp, err := exec.LookPath("gofmt"); err == nil && lp != "" {
		gofmtAvail = true
	} else {
		gofmtAvail = false
	}

	testedToolAvailability = true
}

// parseReferencesOutput parses gopls references output into []Ref. It expects one location per line in the form:
//
//	/abs/path:line:col
//	/abs/path:line:col-endcol
//
// Lines that do not conform are ignored.
func parseReferencesOutput(out string) []Ref {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	results := make([]Ref, 0, len(lines))
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if ref, ok := parseRefLine(ln); ok {
			// Ensure AbsPath is absolute. Some versions of gopls already emit absolute paths.
			if !filepath.IsAbs(ref.AbsPath) {
				if abs, err := filepath.Abs(ref.AbsPath); err == nil {
					ref.AbsPath = abs
				}
			}
			results = append(results, ref)
		}
	}
	return results
}

// parseRefLine parses a single line like:
//
//	/abs/path/to/file.go:247:37-43
//	/abs/path/to/file.go:12:5
//
// Returns (Ref, true) if parsed, otherwise (zero, false).
func parseRefLine(line string) (Ref, bool) {
	// Find the last ":" (separates column from path+line)
	last := strings.LastIndex(line, ":")
	if last <= 0 || last >= len(line)-1 {
		return Ref{}, false
	}
	colPart := line[last+1:]
	pathAndLine := line[:last]

	// Find prior ":" to separate line from path
	last2 := strings.LastIndex(pathAndLine, ":")
	if last2 <= 0 || last2 >= len(pathAndLine)-1 {
		return Ref{}, false
	}
	linePart := pathAndLine[last2+1:]
	pathPart := pathAndLine[:last2]

	// Parse line number
	ln, err := strconv.Atoi(linePart)
	if err != nil || ln <= 0 {
		return Ref{}, false
	}

	// Parse column(s)
	colStart := 0
	colEnd := 0
	if dash := strings.Index(colPart, "-"); dash >= 0 {
		startStr := colPart[:dash]
		endStr := colPart[dash+1:]
		cs, err1 := strconv.Atoi(startStr)
		ce, err2 := strconv.Atoi(endStr)
		if err1 != nil || err2 != nil || cs <= 0 || ce <= 0 {
			return Ref{}, false
		}
		colStart, colEnd = cs, ce
	} else {
		cs, err := strconv.Atoi(colPart)
		if err != nil || cs <= 0 {
			return Ref{}, false
		}
		colStart = cs
		// If no end is provided, treat end as start+1 (byte after start).
		colEnd = cs + 1
	}

	return Ref{
		AbsPath:     pathPart,
		Line:        ln,
		ColumnStart: colStart,
		ColumnEnd:   colEnd,
	}, true
}
