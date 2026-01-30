package gocode

import (
	"bytes"
	"fmt"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Package represents a Go package whose Go files have been read and parsed for documentation. Use Package to generate documentation and analyze the public API.
//
// Most instances are constructed at the Module level. A Package contains all Go source files in a directory, their documentation, and computed doc snippets for exported and unexported
// symbols.
//
// If the package contains black-box tests (`package foo_test`), they are represented by the TestPackage field.
//
// Parsing the files populates PackageDocSnippets, FuncSnippets, ValueSnippets, and TypeSnippets.
//
// Fields for import categorization, documentation, and code elements are described on their respective struct fields below.
type Package struct {
	Name        string              // name given via 'package foo' at the top of Go files
	RelativeDir string              // directory relative to the containing Go module (ex: "foo/bar")
	ImportPath  string              // canonical import path for referring to this package (ex: "myproj/foo/bar")
	TestPackage *Package            // black-box test package (if any): holds files with `package foo_test`
	ImportPaths map[string]struct{} // set of import paths directly imported by this package

	// Memoized categorized import paths. These are filled lazily the first time one of
	// ImportPathsModule/ImportPathsStdlib/ImportPathsVendor is called.

	// importPathsModule is a memoized, deterministically sorted list of import paths that belong to the same module. It is filled lazily by ImportPathsModule().
	importPathsModule []string

	// importPathsStdlib is a memoized, deterministically sorted list of Go standard library import paths used by this package. It is filled lazily by ImportPathsStdlib().
	importPathsStdlib []string

	// importPathsVendor is a memoized, deterministically sorted list of third-party (vendored) import paths used by this package. It is filled lazily by ImportPathsVendor().
	importPathsVendor []string

	// PackageDocSnippets contains the package-level Go documentation comments extracted from each file. There is one entry per file that has a package doc comment, including 'doc.go' and
	// any other files.
	PackageDocSnippets []*PackageDocSnippet

	// FuncSnippets holds documentation snippets describing each function or method declared at the top level inside this package. Entries cover both exported and unexported functions,
	// and interface/receiver methods.
	FuncSnippets []*FuncSnippet

	// ValueSnippets holds documentation snippets for each const and var declaration (including both blocks and single specs), both exported and unexported.
	ValueSnippets []*ValueSnippet

	// UnattachedComments holds comments that are not attached, or included in, any other snippet. If a comment shows up in .FullBytes() of a snippet,
	// it's not unattached. An unattached comment must be top-level and not inside of var/const/type block, not inside a function, etc. Usually it will have a
	// blank line after it (unless the EOF is after it).
	UnattachedComments []*UnattachedComment

	TypeSnippets        []*TypeSnippet            // TypeSnippets holds documentation snippets for all declared types (structs, interfaces, aliases, etc.), both exported and unexported
	identifierToSnippet map[string]Snippet        // mapping from identifier name to the corresponding code/documentation snippet
	Files               map[string]*File          // all Go source files in this package, keyed by filename relative to the package dir
	Module              *Module                   // module containing this package
	typeToMethods       map[string][]*FuncSnippet // typeToMethods groups method and function documentation: type name -> methods/functions. For functions, "none" is used as the key
	parentPackage       *Package                  // if set, denotes this as a test package and points to the parent main package
	parsed              bool                      // true if this package's documentation has been parsed and all fields are populated
}

// HasTestPackage reports whether the package has a test package (black-box tests with package foo_test).
func (p *Package) HasTestPackage() bool {
	return p.TestPackage != nil
}

// IsTestPackage reports whether p represents the external test package (the package named "<name>_test"). It returns true only for that test package; for the main package, even if
// it has external tests, it returns false. For main packages, the corresponding test package (if any) is available via p.TestPackage.
func (p *Package) IsTestPackage() bool {
	return p.parentPackage != nil
}

// NewPackage creates and parses a Go package from the specified files and directory. All supplied .go files are fully read and parsed; no additional files are discovered. The resulting
// Package is initialized with its name, file map, import path, metadata, and (if needed) test package. It returns an error if any file cannot be read, if package names are inconsistent,
// or if standard Go package structure rules are violated. The created package is added to the containing Module's package map.
func NewPackage(relativeDir string, absoluteDirPath string, goFileNames []string, m *Module) (*Package, error) {
	if len(goFileNames) == 0 {
		return nil, fmt.Errorf("no Go files provided")
	}

	importPath := importPathFromRelativeDir(m.Name, relativeDir)

	// First, read all files and create File structs
	allFiles := make(map[string]*File)
	for _, fileName := range goFileNames {
		fullFilePath := filepath.Join(absoluteDirPath, fileName)
		contents, err := os.ReadFile(fullFilePath)
		if err != nil {
			return nil, fmt.Errorf("could not ReadFile: %w", err)
		}

		packageName, err := extractPackageName(contents)
		if err != nil {
			return nil, fmt.Errorf("could not extractPackageName: %w", err)
		}

		goFile := &File{
			FileName:         fileName,
			RelativeFileName: filepath.Join(relativeDir, fileName),
			AbsolutePath:     fullFilePath,
			Contents:         contents,
			PackageName:      packageName,
			IsTest:           strings.HasSuffix(fileName, "_test.go"),
		}

		allFiles[fileName] = goFile
	}

	// Validate the package structure
	hasTestPackage, mainPackageName, err := validateAndDetectTestPackage(allFiles)
	if err != nil {
		return nil, err
	}

	// Segment files into main and test packages
	mainPkgFiles := make(map[string]*File)
	testPkgFiles := make(map[string]*File)
	testPackageName := mainPackageName + "_test"

	for fileName, goFile := range allFiles {
		if hasTestPackage && goFile.PackageName == testPackageName {
			// This file belongs to the black-box test package
			testPkgFiles[fileName] = goFile
		} else {
			// This file belongs to the main package (including white-box tests)
			mainPkgFiles[fileName] = goFile
		}
	}

	// Create the main package
	pkg := &Package{
		Name:          mainPackageName,
		RelativeDir:   relativeDir,
		ImportPath:    importPath,
		Files:         mainPkgFiles,
		typeToMethods: make(map[string][]*FuncSnippet),
		Module:        m,
	}

	// Create the test package if there are black-box test files
	if hasTestPackage && len(testPkgFiles) > 0 {
		testPkg := &Package{
			Name:          testPackageName,
			RelativeDir:   relativeDir,
			ImportPath:    importPath + "_test",
			Files:         testPkgFiles,
			typeToMethods: make(map[string][]*FuncSnippet),
			Module:        m,
			parentPackage: pkg,
		}

		// Parse the test package
		err = testPkg.parse()
		if err != nil {
			return nil, fmt.Errorf("failed to parse test package: %w", err)
		}

		pkg.TestPackage = testPkg

		// NOTE: do NOT add testPkg to m.Packages
	}

	// Parse the main package
	err = pkg.parse()
	if err != nil {
		return nil, err
	}

	// Add this package to m:
	m.Packages[pkg.ImportPath] = pkg

	return pkg, nil
}

// importPathFromRelativeDir joins moduleName and relativeDir into an import path. If relativeDir is empty, moduleName is returned unchanged.
func importPathFromRelativeDir(moduleName, relativeDir string) string {
	importPath := moduleName
	if relativeDir != "" {
		importPath = importPath + "/" + relativeDir
	}
	return importPath
}

// Reload returns a new Package without mutating p, but with all files re-read and re-parsed (new files are not discovered). It mutates p.Module, replacing p with the new package. If
// p has a test package, that package is also reloaded. If p is a test package, its parent package will be reloaded, which in turn reloads p.
func (p *Package) Reload() (*Package, error) {
	// If p is a test package (i.e. has a parentPackage), reload the parent package
	// and return the freshly reloaded test package instance.
	if p.parentPackage != nil {
		newParent, err := p.parentPackage.Reload()
		if err != nil {
			return nil, err
		}
		return newParent.TestPackage, nil
	}

	// p is a main package. Collect the set of Go files that presently compose
	// the package (including any black-box test files that live in p.TestPackage).
	// Do not rescan files.
	fileSet := make(map[string]struct{})
	for _, fn := range p.FileNames() {
		fileSet[fn] = struct{}{}
	}
	if p.TestPackage != nil {
		for _, fn := range p.TestPackage.FileNames() {
			fileSet[fn] = struct{}{}
		}
	}

	// Convert the set to a deterministic, sorted slice.
	goFileNames := make([]string, 0, len(fileSet))
	for fn := range fileSet {
		goFileNames = append(goFileNames, fn)
	}
	sort.Strings(goFileNames)

	// Re-create the Package, which adds newPkg to p.m.Packages:
	newPkg, err := NewPackage(p.RelativeDir, p.AbsolutePath(), goFileNames, p.Module)
	if err != nil {
		return nil, err
	}

	return newPkg, nil
}

// AbsolutePath returns the absolute filesystem path to p's directory by joining p.Module.AbsolutePath and p.RelativeDir using OS-specific path separators. It does not check that the
// path exists or resolve symlinks. p.Module must be non-nil.
func (p *Package) AbsolutePath() string {
	return filepath.Join(p.Module.AbsolutePath, p.RelativeDir)
}

// Snippets returns all snippets known to the package. The result is a concatenation of function, value, type, and package-documentation snippets and is not sorted. It does not return
// snippets in p.TestPackage.
func (p *Package) Snippets() []Snippet {
	var snippets []Snippet

	for _, s := range p.FuncSnippets {
		snippets = append(snippets, s)
	}
	for _, s := range p.ValueSnippets {
		snippets = append(snippets, s)
	}
	for _, s := range p.TypeSnippets {
		snippets = append(snippets, s)
	}
	for _, s := range p.PackageDocSnippets {
		snippets = append(snippets, s)
	}

	return snippets
}

// Clone creates a clone of the package in a temporary directory.
//
// It is sugar for cloning a module and then cloning the package. Clean up the temporary clone by calling DeleteClone on the returned package's Module (ex: newPkg.Module.DeleteClone()).
func (p *Package) Clone() (*Package, error) {
	mClone, err := p.Module.CloneWithoutPackages()
	if err != nil {
		return nil, err
	}
	return mClone.ClonePackage(p)
}

// parse parses the package.
func (p *Package) parse() error {
	if p.parsed {
		return nil // Already parsed, return early to ensure idempotence
	}

	fset := token.NewFileSet()

	// Create and sort a slice of filenames for consistent processing order
	fileNames := make([]string, 0, len(p.Files))
	for fileName := range p.Files {
		fileNames = append(fileNames, fileName)
	}
	sort.Strings(fileNames)

	// Track all unique import paths encountered while parsing the package's files.
	importPaths := make(map[string]struct{})

	// Iterate through all files in the package
	for _, fileName := range fileNames {
		file := p.Files[fileName]

		_, err := file.Parse(fset)
		if err != nil {
			return fmt.Errorf("could not parse file: %w", err)
		}

		// Record any import paths found in this file's AST.
		for _, imp := range file.AST.Imports {
			// imp.Path.Value is a quoted string literal (e.g. "\"fmt\""). Unquote it.
			if path, err := strconv.Unquote(imp.Path.Value); err == nil && path != "" {
				importPaths[path] = struct{}{}
			}
		}

		funcs, values, types, packageDoc, err := extractSnippets(file)
		if err != nil {
			return fmt.Errorf("failed to extract snippets: %w", err)
		}

		if packageDoc != nil {
			p.PackageDocSnippets = append(p.PackageDocSnippets, packageDoc)
		}

		// Append to existing slices instead of overwriting
		p.FuncSnippets = append(p.FuncSnippets, funcs...)
		p.ValueSnippets = append(p.ValueSnippets, values...)
		p.TypeSnippets = append(p.TypeSnippets, types...)
	}

	// After all snippets are collected, compute unattached comments per file
	perFileSnippets := p.SnippetsByFile(nil)
	for _, fileName := range fileNames {
		file := p.Files[fileName]
		ucs, err := extractUnattachedComments(file, perFileSnippets[fileName])
		if err != nil {
			return fmt.Errorf("failed to extract unattached comments: %w", err)
		}
		if len(ucs) > 0 {
			p.UnattachedComments = append(p.UnattachedComments, ucs...)
		}
	}

	// Save the discovered import paths.
	p.ImportPaths = importPaths

	p.typeToMethods = groupFunctionsByType(p.TypeSnippets, p.FuncSnippets)

	// Populate identifierToSnippet map
	p.identifierToSnippet = make(map[string]Snippet)

	// Add function snippets
	for _, fs := range p.FuncSnippets {
		p.identifierToSnippet[fs.Identifier] = fs
	}

	// Add type snippets
	for _, ts := range p.TypeSnippets {
		for _, identifier := range ts.Identifiers {
			p.identifierToSnippet[identifier] = ts
		}
	}

	// Add value snippets
	for _, vs := range p.ValueSnippets {
		for _, identifier := range vs.Identifiers {
			p.identifierToSnippet[identifier] = vs
		}
	}

	// Add package documentation snippets
	for _, ps := range p.PackageDocSnippets {
		p.identifierToSnippet[ps.Identifier] = ps
	}

	if len(p.PackageDocSnippets) > 0 {
		p.identifierToSnippet[PackageIdentifier] = primaryPackageDocSnippet(p)
	}

	p.parsed = true // Mark as parsed

	return nil
}

// WriteDocumentationTo writes package documentation to w. The documentation is human- and AI-readable and consists of exported functions, types, consts, and vars; items without comments
// are still emitted. Types have unexported fields elided.
func (p *Package) WriteDocumentationTo(w io.Writer) (int64, error) {
	// Write all types first, then vars/consts, and finally functions.
	var totalBytes int64

	// Write package name and import path as a header
	header := fmt.Sprintf("// Package %s (Import: %s)\n\n", p.Name, p.ImportPath)
	n, err := io.WriteString(w, header)
	if err != nil {
		return totalBytes, err
	}
	totalBytes += int64(n)

	// Write package-level documentation if available
	for _, docSnippet := range p.PackageDocSnippets {
		if len(docSnippet.Snippet) > 0 {
			n, err = w.Write(docSnippet.Snippet)
			if err != nil {
				return totalBytes, err
			}
			totalBytes += int64(n)

			// Add newline if snippet doesn't end with one
			if len(docSnippet.Snippet) > 0 && docSnippet.Snippet[len(docSnippet.Snippet)-1] != '\n' {
				n, err = io.WriteString(w, "\n")
				if err != nil {
					return totalBytes, err
				}
				totalBytes += int64(n)
			}
		}
	}

	// Write types and their methods
	if len(p.TypeSnippets) > 0 {
		sectionHeader := "//\n// Types and their methods\n//\n\n"
		n, err = io.WriteString(w, sectionHeader)
		if err != nil {
			return totalBytes, err
		}
		totalBytes += int64(n)

		for _, s := range p.TypeSnippets {
			if s.Test() {
				continue
			}

			// Write the type
			ps, err := s.PublicSnippet()
			if err != nil {
				return totalBytes, err
			}
			if ps == nil {
				continue // nothing public
			}

			n, err = w.Write(ps)
			if err != nil {
				return totalBytes, err
			}
			totalBytes += int64(n)
			n, err = w.Write([]byte{'\n', '\n'})
			if err != nil {
				return totalBytes, err
			}
			totalBytes += int64(n)

			// Write all methods of these types
			for _, typeName := range s.Identifiers {
				if methods, ok := p.typeToMethods[typeName]; ok && len(methods) > 0 {
					for _, methodSnippet := range methods {
						if methodSnippet.Test() {
							continue
						}

						ps, err := methodSnippet.PublicSnippet()
						if err != nil {
							return totalBytes, err
						}
						if ps == nil {
							continue // nothing public
						}

						n, err = w.Write(ps)
						if err != nil {
							return totalBytes, err
						}
						totalBytes += int64(n)
						n, err = w.Write([]byte{'\n', '\n'})
						if err != nil {
							return totalBytes, err
						}
						totalBytes += int64(n)
					}
				}
			}
		}
	}

	// Write values (constants and variables)
	if len(p.ValueSnippets) > 0 {
		sectionHeader := "//\n// Constants and Variables\n//\n\n"
		n, err = io.WriteString(w, sectionHeader)
		if err != nil {
			return totalBytes, err
		}
		totalBytes += int64(n)

		for _, s := range p.ValueSnippets {
			if s.Test() {
				continue
			}

			ps, err := s.PublicSnippet()
			if err != nil {
				return totalBytes, err
			}
			if ps == nil {
				continue // nothing public
			}

			n, err = w.Write(ps)
			if err != nil {
				return totalBytes, err
			}
			totalBytes += int64(n)
			n, err = w.Write([]byte{'\n', '\n'})
			if err != nil {
				return totalBytes, err
			}
			totalBytes += int64(n)
		}
	}

	// Write functions without a receiver:
	if methods, ok := p.typeToMethods["none"]; ok && len(methods) > 0 {
		sectionHeader := "//\n// Functions\n//\n\n"
		n, err = io.WriteString(w, sectionHeader)
		if err != nil {
			return totalBytes, err
		}
		totalBytes += int64(n)

		for _, s := range methods {
			if s.Test() {
				continue
			}

			ps, err := s.PublicSnippet()
			if err != nil {
				return totalBytes, err
			}
			if ps == nil {
				continue // nothing public
			}

			n, err = w.Write(ps)
			if err != nil {
				return totalBytes, err
			}
			totalBytes += int64(n)
			n, err = w.Write([]byte{'\n', '\n'})
			if err != nil {
				return totalBytes, err
			}
			totalBytes += int64(n)
		}
	}

	return totalBytes, nil
}

// MarshalDocumentation calls WriteDocumentationTo with a new buffer, then returns the buffer.
func (p *Package) MarshalDocumentation() ([]byte, error) {
	var buf bytes.Buffer
	_, err := p.WriteDocumentationTo(&buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FileNames returns the file names in the package.
func (p *Package) FileNames() []string {
	names := make([]string, 0, len(p.Files))
	for name := range p.Files {
		names = append(names, name)
	}
	return names
}

// GetSnippet returns the snippet for the given identifier, or nil if not found. It does not return snippets in p.TestPackage.
func (p *Package) GetSnippet(identifier string) Snippet {
	return p.identifierToSnippet[identifier]
}

// Identifiers(false) returns all named identifiers, excluding init() functions. Identifiers(true) returns all named, ambiguous, or anonymous identifiers. It does not return identifiers
// in p.TestPackage.
func (p *Package) Identifiers(includeAmbiguous bool) []string {
	var idents []string
	for id := range p.identifierToSnippet {
		if includeAmbiguous {
			idents = append(idents, id)
		} else {
			if !IsAmbiguousIdentifier(id) {
				idents = append(idents, id)
			}
		}
	}
	return idents
}

// PartitionGeneratedIdentifiers partitions identifiers into non-generated and generated identifiers. A generated identifier is defined as an identifier that originated in a generated
// file (as per File.IsCodeGenerated).
//
// Identifiers may include ambiguous identifiers. Any invalid identifier (not returned in p.Identifiers(true)) will be partitioned into the first bucket.
//
// Deprecated: use FilterIdentifiers.
func (p *Package) PartitionGeneratedIdentifiers(identifiers []string) ([]string, []string) {
	var nonGenerated []string
	var generated []string

	for _, id := range identifiers {
		s := p.GetSnippet(id)
		if s == nil {
			// Unknown/invalid identifier: place into the first bucket
			nonGenerated = append(nonGenerated, id)
			continue
		}

		fileName := s.Position().Filename
		f := p.Files[fileName]
		if f != nil && f.IsCodeGenerated() {
			generated = append(generated, id)
		} else {
			nonGenerated = append(nonGenerated, id)
		}
	}

	return nonGenerated, generated
}

// IdentifiersInFile returns identifiers associated with fileName. If includeAmbiguous is false, ambiguous names (ex: "_" and init identifiers) are excluded. It does not return identifiers
// in p.TestPackage, even if fileName is in TestPackage.
//
// Deprecated: use FilterIdentifiers.
func (p *Package) IdentifiersInFile(fileName string, includeAmbiguous bool) []string {
	var idents []string

	for _, s := range p.Snippets() {
		if s.Position().Filename == fileName {
			idents = append(idents, s.IDs()...)
		}
	}

	var filtered []string
	for _, id := range idents {
		if includeAmbiguous {
			filtered = append(filtered, id)
		} else {
			if !IsAmbiguousIdentifier(id) {
				filtered = append(filtered, id)
			}
		}
	}
	return filtered
}

// FilterIdentifiersOptions are options for FilterIdentifiers that control the allowed types of identifiers to return. They do not include identifiers in p.TestPackage.
//
// By default: test files (but no TestXxx/etc funcs); no generated files; no ambiguous identifiers; all snippet kinds
type FilterIdentifiersOptions struct {
	Files                []string // if present, only include identifiers in these files (non-existent files are ignored)
	NoTests              bool     // if true, no identifiers in test files will be included (even if Files includes a test file)
	IncludeTestFuncs     bool     // if true, we include testing funcs (ex: TestXxx, BenchmarkXxx, FuzzXxx, Example), otherwise we exclude them
	IncludeGeneratedFile bool     // if true, we include ids from generated files, otherwise exclude them
	IncludeAmbiguous     bool     // if true, we include ambiguous ids, otherwise we don't
	OnlyAnyDocs          bool     // if true, only include identifiers if it has any docs per IDIsDocumented, otherwise docs is a non-factor

	// If all IncludeSnippetTypeX are false, we include all types

	IncludeSnippetFuncs       bool
	IncludeSnippetType        bool
	IncludeSnippetValue       bool
	IncludeSnippetVar         bool
	IncludeSnippetConst       bool
	IncludeSnippetPackageDocs bool
}

// Common options for FilterIdentifiers.
var (
	FilterIdentifiersOptionsAll                    = FilterIdentifiersOptions{IncludeTestFuncs: true, IncludeGeneratedFile: true, IncludeAmbiguous: true}
	FilterIdentifiersOptionsAllNonGenerated        = FilterIdentifiersOptions{IncludeTestFuncs: true, IncludeGeneratedFile: false, IncludeAmbiguous: true}
	FilterIdentifiersOptionsNonAmbiguous           = FilterIdentifiersOptions{IncludeTestFuncs: true, IncludeGeneratedFile: true, IncludeAmbiguous: false}
	FilterIdentifiersOptionsDocumentedNonAmbiguous = FilterIdentifiersOptions{IncludeTestFuncs: true, IncludeGeneratedFile: true, IncludeAmbiguous: false, OnlyAnyDocs: true}
)

// FilterIdentifiers returns identifiers that match specific criteria (ex: only non-test files, types and functions, not in generated files).
//
// If identifiers is present, it filters them based on options. Otherwise, it filters all identifiers in the package. Invalid identifiers are always filtered out.
func (p *Package) FilterIdentifiers(identifiers []string, options FilterIdentifiersOptions) []string {
	// Helper: determine if snippet kind is allowed by options.
	kindAllowed := func(snippet Snippet) bool {
		// If no kind filters are specified, include all kinds by default.
		noneSet := !options.IncludeSnippetFuncs &&
			!options.IncludeSnippetType &&
			!options.IncludeSnippetValue &&
			!options.IncludeSnippetVar &&
			!options.IncludeSnippetConst &&
			!options.IncludeSnippetPackageDocs

		if noneSet {
			return true
		}

		switch s := snippet.(type) {
		case *FuncSnippet:
			return options.IncludeSnippetFuncs
		case *TypeSnippet:
			return options.IncludeSnippetType
		case *ValueSnippet:
			if options.IncludeSnippetValue {
				return true
			}
			if s.IsVar {
				return options.IncludeSnippetVar
			}
			return options.IncludeSnippetConst
		case *PackageDocSnippet:
			return options.IncludeSnippetPackageDocs
		default:
			return false
		}
	}

	// Build file-name allowlist if provided.
	var fileAllow map[string]struct{}
	if len(options.Files) > 0 {
		fileAllow = make(map[string]struct{}, len(options.Files))
		for _, fn := range options.Files {
			if fn == "" {
				continue
			}
			fileAllow[fn] = struct{}{}
		}
	}

	// Helper: apply all filters to a given id/snippet pair.
	passes := func(id string, snippet Snippet) bool {
		// Ambiguous identifiers (init, anonymous) unless explicitly included.
		if !options.IncludeAmbiguous && IsAmbiguousIdentifier(id) {
			return false
		}

		// File allowlist, if any.
		if fileAllow != nil {
			if _, ok := fileAllow[snippet.Position().Filename]; !ok {
				return false
			}
		}

		// Exclude test-file snippets if requested.
		if options.NoTests && snippet.Test() {
			return false
		}

		// Exclude testing funcs (Test/Benchmark/Fuzz/Example) unless explicitly included.
		if !options.IncludeTestFuncs {
			if fs, ok := snippet.(*FuncSnippet); ok {
				if fs.IsTestFunc() {
					return false
				}
			}
		}

		// Exclude generated files unless explicitly included.
		if !options.IncludeGeneratedFile {
			fileName := snippet.Position().Filename
			if f := p.Files[fileName]; f != nil && f.IsCodeGenerated() {
				return false
			}
		}

		// Kind filter.
		if !kindAllowed(snippet) {
			return false
		}

		// Documentation filter: if requested, require that the identifier has any docs.
		if options.OnlyAnyDocs {
			anyDocs, _ := IDIsDocumented(snippet, id, true)
			if !anyDocs {
				return false
			}
		}

		return true
	}

	var result []string

	if len(identifiers) > 0 {
		// Filter only the provided identifiers; drop invalid ones.
		for _, id := range identifiers {
			s := p.GetSnippet(id)
			if s == nil {
				continue // invalid identifier: always filtered out
			}
			if passes(id, s) {
				result = append(result, id)
			}
		}
		return result
	}

	// Otherwise, consider all identifiers in the package (excluding p.TestPackage by construction).
	for _, s := range p.Snippets() {
		for _, id := range s.IDs() {
			if passes(id, s) {
				result = append(result, id)
			}
		}
	}

	return result
}

// validateAndDetectTestPackage returns true if and only if the collection contains both a "normal" package `foo` (at least one non-test file or a white-box test that declares `package foo`)
// and a black-box test package `foo_test`.
//
// It is legal for a directory to contain only `*_test.go` files, provided they are consistent:
//   - All test files must belong to exactly one base package `foo` (either `package foo` or `package foo_test`).
//   - A `*_test.go` file may declare either `foo` (white-box test) or `foo_test` (black-box test); nothing else.
//
// Note that it is valid for a package to have the names `foo_test` and `foo_test_test`. Errors are reported for mixed base packages or mismatched names.
//
// The function returns:
//   - hasTestPackage: true iff both `foo` and `foo_test` are present; false if the directory contains only one of them (including the case of only `foo_test` files, which is treated
//     as a normal package).
//   - mainPackageName: the name of the main package.
//   - error: any validation errors encountered
func validateAndDetectTestPackage(files map[string]*File) (bool, string, error) {
	var basePkg string // canonical package name (foo)

	// First pass: establish basePkg from non‑test (*.go) files, if any.
	for _, f := range files {
		if f.PackageName == "" {
			return false, "", fmt.Errorf("file %q has no package name", f.FileName)
		}
		if !f.IsTest {
			if basePkg == "" {
				basePkg = f.PackageName
			} else if f.PackageName != basePkg {
				return false, "", fmt.Errorf("mixed non-test packages: %q and %q", basePkg, f.PackageName)
			}
		}
	}

	if basePkg == "" {
		// If there are no non-test files:

		var pkgA, pkgB string

		for _, f := range files {
			if !f.IsTest {
				continue
			}
			if pkgA == "" {
				pkgA = f.PackageName
			} else if pkgA == f.PackageName {
				// normal
			} else if pkgB == "" {
				pkgB = f.PackageName
			} else if pkgB == f.PackageName {
				// normal
			} else {
				return false, "", fmt.Errorf("three test packages declared: %q, %q, %q", pkgA, pkgB, f.PackageName)
			}
		}

		if pkgB == "" {
			return false, pkgA, nil // only one package identifier was found
		} else if pkgA == (pkgB + "_test") {
			return true, pkgB, nil
		} else if pkgB == (pkgA + "_test") {
			return true, pkgA, nil
		} else {
			return false, "", fmt.Errorf("there were only test files. package names must be of the form foo and foo_bar. Got: %q and %q", pkgA, pkgB)
		}
	} else {
		// At this point, we know all non-test files have the same package name.
		// We just need to ensure all test files either have the same package name or _test suffix.
		expectedTestPkgName := basePkg + "_test"
		foundTestPkg := false
		for _, f := range files {
			if !f.IsTest {
				continue
			}

			if f.PackageName == basePkg {
				// normal
			} else if f.PackageName == expectedTestPkgName {
				foundTestPkg = true
			} else {
				return false, "", fmt.Errorf("test file has non-conforming package name: base=%q test_file=%q", basePkg, f.PackageName)
			}
		}
		return foundTestPkg, basePkg, nil
	}
}

// ImportPathsModule returns the import paths that belong to the current module (ex: they share the module path prefix). The result is memoized and returned in a deterministic, sorted
// order.
func (p *Package) ImportPathsModule() []string {
	p.categorizeImportPaths()
	return p.importPathsModule
}

// ImportPathsStdlib returns the list of standard library import paths this package depends on. The result is memoized and returned in a deterministic, sorted order.
func (p *Package) ImportPathsStdlib() []string {
	p.categorizeImportPaths()
	return p.importPathsStdlib
}

// ImportPathsVendor returns the list of third-party (vendored) import paths (ex: any import that is not in the standard library and does not belong to the current module). The result
// is memoized and returned in a deterministic, sorted order.
func (p *Package) ImportPathsVendor() []string {
	p.categorizeImportPaths()
	return p.importPathsVendor
}

// categorizeImportPaths populates the memoized slices. It is safe to call multiple times; repeated calls are no-ops once the slices are populated.
func (p *Package) categorizeImportPaths() {
	// If memoised already, nothing to do.
	if p.importPathsModule != nil || p.importPathsStdlib != nil || p.importPathsVendor != nil {
		return
	}

	modSet := make(map[string]struct{})
	stdSet := make(map[string]struct{})
	venSet := make(map[string]struct{})

	// Prepare list of import paths to query.
	var paths []string
	for path := range p.ImportPaths {
		paths = append(paths, path)
	}

	if len(paths) == 0 {
		// No imports – memoise empty (but non-nil) slices and return.
		p.importPathsModule = []string{}
		p.importPathsStdlib = []string{}
		p.importPathsVendor = []string{}
		return
	}

	cfg := &packages.Config{
		Mode: packages.NeedModule,
		Dir:  p.Module.AbsolutePath,
		Env:  os.Environ(),
	}

	pkgs, err := packages.Load(cfg, paths...)
	if err != nil {
		fmt.Println("WARNING: error during packages.Load", err)
	}

	// Build map from PkgPath to *packages.Package for quick lookup.
	pkgMap := make(map[string]*packages.Package, len(pkgs))
	for _, pkg := range pkgs {
		if pkg.PkgPath != "" {
			pkgMap[pkg.PkgPath] = pkg
		}
	}

	for _, path := range paths {
		pkg := pkgMap[path]

		// Classification using go/packages result first.
		if pkg != nil && pkg.Module != nil {
			if p.Module != nil && pkg.Module.Path == p.Module.Name {
				modSet[path] = struct{}{}
			} else {
				venSet[path] = struct{}{}
			}
			continue
		}

		// If pkg.Module == nil, it's likely stdlib.
		if pkg != nil && pkg.Module == nil {
			stdSet[path] = struct{}{}
			continue
		}

		// Fallback heuristics (in case loading failed).
		if p.Module != nil && strings.HasPrefix(path, p.Module.Name) {
			modSet[path] = struct{}{}
		} else if strings.Contains(path, ".") {
			venSet[path] = struct{}{}
		} else {
			stdSet[path] = struct{}{}
		}
	}

	// mapKeysSorted returns the keys of m, sorted lexicographically.
	mapKeysSorted := func(m map[string]struct{}) []string {
		if len(m) == 0 {
			return []string{}
		}
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}

	p.importPathsModule = mapKeysSorted(modSet)
	p.importPathsStdlib = mapKeysSorted(stdSet)
	p.importPathsVendor = mapKeysSorted(venSet)
}
