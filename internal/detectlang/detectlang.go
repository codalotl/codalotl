package detectlang

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Lang represents a detected programming language.
type Lang string

const (
	LangUnknown    Lang = ""
	LangMultiple   Lang = "multiple"
	LangGo         Lang = "go"
	LangRuby       Lang = "rb"
	LangPython     Lang = "py"
	LangRust       Lang = "rs"
	LangJavaScript Lang = "js"
	LangTypeScript Lang = "ts"
	LangJava       Lang = "java"
	LangC          Lang = "c"
	LangCpp        Lang = "cpp"
	LangCSharp     Lang = "cs"
	LangPHP        Lang = "php"
	LangSwift      Lang = "swift"
	LangKotlin     Lang = "kt"
	LangScala      Lang = "scala"
	LangObjectiveC Lang = "objc"
)

var (
	errPathNotAbsolute = errors.New("detectlang: both absRootDir and absPath must be absolute")
	errPathOutsideRoot = errors.New("detectlang: absPath must be within absRootDir")
)

var extToLang = map[string]Lang{
	".go":    LangGo,
	".rb":    LangRuby,
	".py":    LangPython,
	".rs":    LangRust,
	".js":    LangJavaScript,
	".mjs":   LangJavaScript,
	".cjs":   LangJavaScript,
	".jsx":   LangJavaScript,
	".ts":    LangTypeScript,
	".tsx":   LangTypeScript,
	".java":  LangJava,
	".c":     LangC,
	".cpp":   LangCpp,
	".cc":    LangCpp,
	".cxx":   LangCpp,
	".hpp":   LangCpp,
	".hh":    LangCpp,
	".hxx":   LangCpp,
	".cs":    LangCSharp,
	".csx":   LangCSharp,
	".php":   LangPHP,
	".phtml": LangPHP,
	".swift": LangSwift,
	".kt":    LangKotlin,
	".kts":   LangKotlin,
	".scala": LangScala,
	".m":     LangObjectiveC,
	".mm":    LangObjectiveC,
}

// Detect detects the programming language indicated by absPath (which must be absolute). The path can either be to a file or a directory.
//   - When used on a file, only looks at that file. Returns LangUnknown or a specific language. Never LangMultiple. Otherwise...
//   - If the directory has a dominant language by plurality, we return that language (ex: a bunch of .go files and one .rb file). Otherwise...
//   - If the directory has an equal amount of some set of languages, return LangMultiple.
//   - If the directory has no files at all, or no files with a known extension, iteratively check nearby dirs in BFS manner (ex: parent, children, parent's parent,
//     children's children, parent's children, etc). Stop when we get language that is not LangUnknown. If the BFS exhausts without finding a known language, return
//     LangUnknown. Does not traverse beyond absRootDir.
//
// An error is returned if absPath isn't absolute or not in absRootDir, if the path does not exist, or if some other I/O error occurs.
func Detect(absRootDir, absPath string) (Lang, error) {
	root := filepath.Clean(absRootDir)
	target := filepath.Clean(absPath)

	if !filepath.IsAbs(root) || !filepath.IsAbs(target) {
		return LangUnknown, errPathNotAbsolute
	}

	rootInfo, err := os.Stat(root)
	if err != nil {
		return LangUnknown, fmt.Errorf("detectlang: stat root: %w", err)
	}
	if !rootInfo.IsDir() {
		return LangUnknown, fmt.Errorf("detectlang: absRootDir is not a directory: %s", absRootDir)
	}

	if !withinRoot(root, target) {
		return LangUnknown, errPathOutsideRoot
	}

	info, err := os.Stat(target)
	if err != nil {
		return LangUnknown, fmt.Errorf("detectlang: stat target: %w", err)
	}

	if info.IsDir() {
		return detectDir(root, target)
	}

	return langForExt(filepath.Ext(target)), nil
}

func detectDir(root, dir string) (Lang, error) {
	type dirNode struct {
		path string
	}

	queue := []dirNode{{path: dir}}
	visited := map[string]struct{}{
		dir: {},
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		lang, hasKnown, subdirs, err := analyzeDir(current.path)
		if err != nil {
			return LangUnknown, err
		}

		if hasKnown && lang != LangUnknown {
			return lang, nil
		}

		for _, neighbor := range directoryNeighbors(root, current.path, subdirs) {
			if _, seen := visited[neighbor]; seen {
				continue
			}
			visited[neighbor] = struct{}{}
			queue = append(queue, dirNode{path: neighbor})
		}
	}

	return LangUnknown, nil
}

func analyzeDir(dir string) (Lang, bool, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return LangUnknown, false, nil, fmt.Errorf("detectlang: read dir %s: %w", dir, err)
	}

	counts := make(map[Lang]int)
	subdirs := make([]string, 0, len(entries))

	for _, entry := range entries {
		switch {
		case entry.IsDir():
			subdirs = append(subdirs, filepath.Join(dir, entry.Name()))
		default:
			lang := langForExt(filepath.Ext(entry.Name()))
			if lang != LangUnknown {
				counts[lang]++
			}
		}
	}

	if len(counts) == 0 {
		return LangUnknown, false, subdirs, nil
	}

	maxCount := 0
	var best Lang
	tied := false
	for lang, count := range counts {
		if count > maxCount {
			maxCount = count
			best = lang
			tied = false
			continue
		}
		if count == maxCount {
			tied = true
		}
	}

	if tied {
		return LangMultiple, true, subdirs, nil
	}

	return best, true, subdirs, nil
}

func directoryNeighbors(root, current string, subdirs []string) []string {
	var neighbors []string

	if current != root {
		parent := filepath.Dir(current)
		if withinRoot(root, parent) {
			neighbors = append(neighbors, parent)
		}
	}

	for _, subdir := range subdirs {
		if withinRoot(root, subdir) {
			neighbors = append(neighbors, subdir)
		}
	}

	return neighbors
}

func langForExt(ext string) Lang {
	if lang, ok := extToLang[strings.ToLower(ext)]; ok {
		return lang
	}
	return LangUnknown
}

func withinRoot(root, target string) bool {
	if root == target {
		return true
	}

	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." {
		return false
	}
	prefix := ".." + string(os.PathSeparator)
	return !strings.HasPrefix(rel, prefix)
}
