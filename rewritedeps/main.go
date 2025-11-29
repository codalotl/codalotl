package main

import (
	"bytes"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var prefixMap = []struct {
	old string
	new string
}{
	{"axi/codeai/", "github.com/codalotl/codalotl/internal/"},
	{"axi/q/", "github.com/codalotl/codalotl/internal/q/"},
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		log.Fatalf("getwd: %v", err)
	}

	var filesChanged int
	var importsUpdated int

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && shouldSkipDir(d.Name()) {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		changed, count, err := rewriteImports(path)
		if err != nil {
			return err
		}
		if changed {
			filesChanged++
			importsUpdated += count
		}
		return nil
	})
	if err != nil {
		log.Fatalf("walk: %v", err)
	}

	log.Printf("updated %d imports across %d files", importsUpdated, filesChanged)
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "vendor":
		return true
	default:
		return false
	}
}

func rewriteImports(path string) (bool, int, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, 0, err
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return false, 0, err
	}

	var changed bool
	var replacements int

	for _, imp := range file.Imports {
		val, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return false, 0, err
		}
		for _, prefix := range prefixMap {
			if strings.HasPrefix(val, prefix.old) {
				imp.Path.Value = strconv.Quote(prefix.new + strings.TrimPrefix(val, prefix.old))
				changed = true
				replacements++
				break
			}
		}
	}

	if !changed {
		return false, 0, nil
	}

	var buf bytes.Buffer
	cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&buf, fset, file); err != nil {
		return false, 0, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return false, 0, err
	}
	if err := os.WriteFile(path, buf.Bytes(), info.Mode()); err != nil {
		return false, 0, err
	}

	return true, replacements, nil
}
