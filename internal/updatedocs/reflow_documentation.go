package updatedocs

import (
	"bytes"
	"fmt"

	"github.com/codalotl/codalotl/internal/gocode"

	"os"
	"path/filepath"
	"sort"
)

// ReflowDocumentationPaths reflows documentation in paths. Each path is either a dir (nonrecursive) or an individual Go source file. This function is meant to operate
// similarly to `gofmt -l -w` (obviously there are differences).
//
// See ReflowDocumentation for details on what reflowing means.
//
// If dryRun is true, this function will still compute which files would be modified, but it will leave the on-disk contents unchanged.
//
// It returns:
//   - a list of modified files (or nil of nothing was modified).
//   - a list of identifiers that failed reflow.
//   - an overall error, if any. NOTE: failed identifiers do NOT cause an overall error. An overall error is returned for things like I/O errors.
func ReflowDocumentationPaths(paths []string, dryRun bool, options ...Options) (modifiedFiles []string, failedIdentifiers []string, fnErr error) {
	if len(paths) == 0 {
		return nil, nil, nil
	}

	// Extract options and set defaults:
	var opts Options
	if len(options) > 0 {
		opts.ReflowMaxWidth = options[0].ReflowMaxWidth
		opts.ReflowTabWidth = options[0].ReflowTabWidth
	}
	opts.Reflow = true // this function definitionally reflows

	modifiedSet := map[string]struct{}{} // absolute file path -> exists
	failedSet := map[string]struct{}{}   // identifier -> exists

	collect := func(err error) ([]string, []string, error) {
		var modifiedFiles []string
		var failedIdentifiers []string

		for f := range modifiedSet {
			modifiedFiles = append(modifiedFiles, f)
		}
		for id := range failedSet {
			failedIdentifiers = append(failedIdentifiers, id)
		}

		sort.Strings(modifiedFiles)
		sort.Strings(failedIdentifiers)

		if len(modifiedFiles) == 0 {
			modifiedFiles = nil
		}
		if len(failedIdentifiers) == 0 {
			failedIdentifiers = nil
		}

		return modifiedFiles, failedIdentifiers, err
	}

	type pkgTarget struct {
		absDir string // absolute directory containing the package
		all    bool
		files  map[string]struct{} // base names only; ignored if all is true
	}

	targets := map[string]*pkgTarget{} // abs package dir -> target

	// Resolve paths into package targets.
	for _, p := range paths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			return collect(err)
		}

		info, err := os.Stat(absPath)
		if err != nil {
			return collect(err)
		}

		if info.IsDir() {
			// If it's an empty folder (no .go files), treat as a no-op, like gofmt would.
			hasGo, err := dirHasGoFiles(absPath)
			if err != nil {
				return collect(err)
			}
			if !hasGo {
				continue
			}

			t := targets[absPath]
			if t == nil {
				t = &pkgTarget{absDir: absPath}
				targets[absPath] = t
			}
			t.all = true
			t.files = nil
			continue
		}

		if filepath.Ext(absPath) != ".go" {
			return collect(fmt.Errorf("path is not a Go source file: %s", absPath))
		}

		absDir := filepath.Dir(absPath)
		fileName := filepath.Base(absPath)

		t := targets[absDir]
		if t == nil {
			t = &pkgTarget{absDir: absDir, files: map[string]struct{}{}}
			targets[absDir] = t
		}
		if t.all {
			continue
		}
		if t.files == nil {
			t.files = map[string]struct{}{}
		}
		t.files[fileName] = struct{}{}
	}

	var targetDirs []string
	for dir := range targets {
		targetDirs = append(targetDirs, dir)
	}
	sort.Strings(targetDirs)

	moduleCache := map[string]*gocode.Module{} // module abs dir -> module

	for _, absDir := range targetDirs {
		if err := func() (err error) {
			t := targets[absDir]

			m, err := gocode.NewModule(absDir)
			if err != nil {
				return err
			}
			mod := moduleCache[m.AbsolutePath]
			if mod == nil {
				moduleCache[m.AbsolutePath] = m
				mod = m
			}

			pkgRelDir, err := filepath.Rel(mod.AbsolutePath, absDir)
			if err != nil {
				return err
			}
			if pkgRelDir == "" {
				pkgRelDir = "."
			}

			pkg, err := mod.LoadPackageByRelativeDir(pkgRelDir)
			if err != nil {
				return err
			}

			// Select files and take pre-reflow snapshots.
			beforeByAbsPath := map[string][]byte{}
			addBefore := func(absPath string) error {
				if _, ok := beforeByAbsPath[absPath]; ok {
					return nil
				}
				b, err := os.ReadFile(absPath)
				if err != nil {
					return err
				}
				beforeByAbsPath[absPath] = b
				return nil
			}

			var mainFiles []string
			var testFiles []string

			if t.all {
				for _, f := range pkg.Files {
					if err := addBefore(f.AbsolutePath); err != nil {
						return err
					}
				}
				if pkg.HasTestPackage() {
					for _, f := range pkg.TestPackage.Files {
						if err := addBefore(f.AbsolutePath); err != nil {
							return err
						}
					}
				}
			} else {
				for fileName := range t.files {
					if f, ok := pkg.Files[fileName]; ok {
						mainFiles = append(mainFiles, fileName)
						if err := addBefore(f.AbsolutePath); err != nil {
							return err
						}
						continue
					}
					if pkg.HasTestPackage() {
						if f, ok := pkg.TestPackage.Files[fileName]; ok {
							testFiles = append(testFiles, fileName)
							if err := addBefore(f.AbsolutePath); err != nil {
								return err
							}
							continue
						}
					}
					return fmt.Errorf("file %q not found in package %s", fileName, pkg.AbsolutePath())
				}
			}

			sort.Strings(mainFiles)
			sort.Strings(testFiles)

			diffAndRecord := func() error {
				for absPath, before := range beforeByAbsPath {
					after, err := os.ReadFile(absPath)
					if err != nil {
						return err
					}
					if !bytes.Equal(before, after) {
						modifiedSet[absPath] = struct{}{}
					}
				}
				return nil
			}

			restore := func() error {
				if !dryRun {
					return nil
				}
				for absPath, before := range beforeByAbsPath {
					after, err := os.ReadFile(absPath)
					if err != nil {
						return err
					}
					if bytes.Equal(before, after) {
						continue
					}
					if err := os.WriteFile(absPath, before, 0o666); err != nil {
						return err
					}
				}
				return nil
			}

			// Ensure we record diffs for return values, and (optionally) roll back any file writes.
			// Note: when dryRun is true, these must run in this order: diff first, then restore.
			defer func() {
				if err2 := restore(); err == nil && err2 != nil {
					err = err2
				}
			}()
			defer func() {
				if err2 := diffAndRecord(); err == nil && err2 != nil {
					err = err2
				}
			}()

			// Reflow main package identifiers.
			{
				var filter gocode.FilterIdentifiersOptions
				filter.IncludeTestFuncs = true
				filter.OnlyAnyDocs = true
				if !t.all {
					filter.Files = mainFiles
				}

				mainIDs := pkg.FilterIdentifiers(nil, filter)
				updatedPkg, failed, err := ReflowDocumentation(pkg, mainIDs, opts)
				if err != nil {
					return err
				}
				for _, id := range failed {
					failedSet[id] = struct{}{}
				}
				if updatedPkg != nil {
					pkg = updatedPkg
				}
			}

			// Reflow external test package identifiers, if any.
			if pkg.HasTestPackage() {
				var filter gocode.FilterIdentifiersOptions
				filter.IncludeTestFuncs = true
				filter.OnlyAnyDocs = true
				if !t.all {
					filter.Files = testFiles
				}

				testIDs := pkg.TestPackage.FilterIdentifiers(nil, filter)
				_, failed, err := ReflowDocumentation(pkg.TestPackage, testIDs, opts)
				if err != nil {
					return err
				}
				for _, id := range failed {
					failedSet[id] = struct{}{}
				}
			}

			return nil
		}(); err != nil {
			return collect(err)
		}
	}

	return collect(nil)
}

// ReflowAllDocumentation reflows all documentation in a package (including its _test package, if present). It does not reflow generated files.
//
// See ReflowDocumentation for details.
func ReflowAllDocumentation(pkg *gocode.Package, options ...Options) (*gocode.Package, []string, error) {
	nonGenerated, _ := pkg.PartitionGeneratedIdentifiers(pkg.Identifiers(false))
	newPkg, failed, err := ReflowDocumentation(pkg, nonGenerated, options...)
	if err != nil {
		return newPkg, failed, err
	}

	if newPkg.HasTestPackage() {
		testNonGenerated, _ := newPkg.TestPackage.PartitionGeneratedIdentifiers(newPkg.TestPackage.Identifiers(false))
		_, testFailed, err := ReflowDocumentation(newPkg.TestPackage, testNonGenerated, options...)
		failed = append(failed, testFailed...)
		if err != nil {
			return newPkg, failed, err
		}
	}

	return newPkg, failed, nil
}

// ReflowDocumentation will reflow identifiers' documentation in pkg (nil/empty identifiers reflows nothing). Reflowing means three things:
//  1. Wrap text at options.ReflowMaxWidth (also unwrap if it was previously wrapped at lesser width).
//  2. Convert fields/specs to EOL vs. Doc comments based on whether they can fit, and based on maximizing uniformity (ex: if everything is a .Doc comment except
//     for one field, make that field a .Doc comment as well).
//  3. Adjust newline whitespace within struct types and value blocks (ex: a .Doc comment should have a blank line above it, not code).
//
// options is only used for ReflowMaxWidth and ReflowTabWidth - other fields are unused. It returns a reloaded Package if anything was modified (just like UpdateDocumentation),
// any identifiers that were NOT successfully reflowed, and any hard error (ex: I/O error). Identifiers that were not successfully reflowed will NOT cause this function
// to return an error.
func ReflowDocumentation(pkg *gocode.Package, identifiers []string, options ...Options) (*gocode.Package, []string, error) {
	if len(identifiers) == 0 {
		return pkg, nil, nil
	}

	// Extract options and set defaults:
	var opts Options
	if len(options) > 0 {
		opts.ReflowMaxWidth = options[0].ReflowMaxWidth
		opts.ReflowTabWidth = options[0].ReflowTabWidth
	}
	opts.Reflow = true // this function definitionally reflows

	// Collect failed identifiers here:
	var failedIdentifiers []string

	// Construct snippets from identifiers:
	// Note that multiple identifiers can map to the same snippet (ex: var blocks).
	// Keep track of which identifier resulted in which snippet, because snippet errors that come back from UpdateDocumentation are based on the snippet text, not the ID,
	// but we want to map that to identifier errors.
	type snippetWithIdentifiers struct {
		snippet     gocode.Snippet
		str         string
		identifiers []string
	}
	snippetMap := map[gocode.Snippet]*snippetWithIdentifiers{}
	for _, id := range identifiers {
		snippet := pkg.GetSnippet(id)
		if snippet == nil {
			failedIdentifiers = append(failedIdentifiers, id)
			continue
		}

		// Skip snippets with no documentation
		if len(snippet.Docs()) == 0 {
			continue
		}

		existing, ok := snippetMap[snippet]
		if ok {
			existing.identifiers = append(existing.identifiers, id)
		} else {
			snippetMap[snippet] = &snippetWithIdentifiers{
				snippet:     snippet,
				str:         string(snippet.Bytes()),
				identifiers: []string{id},
			}
		}
	}
	if len(snippetMap) == 0 {
		return pkg, failedIdentifiers, nil
	}

	// Call UpdateDocumentation:
	var snippets []string
	for _, v := range snippetMap {
		snippets = append(snippets, v.str)
	}

	newPkg, _, snippetErrs, err := UpdateDocumentation(pkg, snippets, opts)
	if err != nil {
		return newPkg, nil, err
	}

	// Map snippetErrs to failedIdentifiers:
	if len(snippetErrs) > 0 {
		snippetStrToStruct := map[string]*snippetWithIdentifiers{}
		for _, v := range snippetMap {
			snippetStrToStruct[v.str] = v
		}
		for _, sn := range snippetErrs {
			// fmt.Println("Snippet error:")
			// fmt.Println(sn.Snippet)
			// fmt.Println("---- ERROR: ", sn.UserErrorMessage)
			if snippet, ok := snippetStrToStruct[sn.Snippet]; ok {
				failedIdentifiers = append(failedIdentifiers, snippet.identifiers...)
			}
		}
	}

	return newPkg, failedIdentifiers, nil
}

func dirHasGoFiles(absDir string) (bool, error) {
	ents, err := os.ReadDir(absDir)
	if err != nil {
		return false, err
	}
	for _, ent := range ents {
		if ent.IsDir() {
			continue
		}
		if filepath.Ext(ent.Name()) == ".go" {
			return true, nil
		}
	}
	return false, nil
}
