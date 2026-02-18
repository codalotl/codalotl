package updatedocs

import (
	"bytes"
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"go/ast"
	"go/format"
	"regexp"
	"strings"
)

// RemoveDocumentation removes package-level documentation across files in pkg and any associated test packages, and writes any modified files to disk. If no file
// is changed, it returns pkg unchanged; otherwise it reloads and returns the updated package. See RemoveDocumentationInFile for more specific rules.
//
// The identifiers slice controls what to strip. If identifiers is empty, all package-level documentation is removed (including the package comment; use gocode.PackageIdentifier
// to target it). Otherwise, the specified identifiers are stripped.
//
// Only files that change are written. An error is returned if pkg is nil, if a file fails to update or persist, or if reloading the package fails.
func RemoveDocumentation(pkg *gocode.Package, identifiers []string) (*gocode.Package, error) {
	if pkg == nil {
		return nil, fmt.Errorf("package is nil")
	}

	var anyModified bool
	err := gocode.EachPackageWithIdentifiers(pkg, identifiers, gocode.FilterIdentifiersOptionsAll, gocode.FilterIdentifiersOptionsAll, func(p *gocode.Package, ids []string, onlyTests bool) error {

		// RemoveDocumentationInFile wants nil identifiers to remove everything.
		idsToRemove := ids
		if len(identifiers) == 0 {
			idsToRemove = nil
		}

		// Process each file in the package
		var pkgModified bool
		for fileName, file := range p.Files {
			// Remove documentation from the file
			modified, err := RemoveDocumentationInFile(file, idsToRemove)
			if err != nil {
				return fmt.Errorf("failed to remove documentation from %s: %w", fileName, err)
			}

			if modified {
				// Save the modified file to disk
				err = file.PersistContents(false)
				if err != nil {
					return fmt.Errorf("failed to persist %s: %w", fileName, err)
				}
				pkgModified = true
			}
		}

		if pkgModified {
			anyModified = true
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// If no files were modified, return the original package
	if !anyModified {
		return pkg, nil
	}

	// Create a new package with the updated files
	newPkg, err := pkg.Reload()
	if err != nil {
		return nil, fmt.Errorf("failed to create new package: %w", err)
	}

	return newPkg, nil
}

// RemoveDocumentationInFile removes documentation attached to package-level identifiers from file.AST and updates file.Contents. It does not write the file to disk.
//
// If identifiers is nil, documentation for all package-level identifiers is removed, including the package comment (use gocode.PackageIdentifier to refer to it).
// If identifiers is non-nil, only identifiers named in the slice are affected; an empty slice removes nothing.
//
// The function removes:
//   - Leading Doc comments on the package (file.AST.Doc), GenDecls, ValueSpecs, TypeSpecs, and FuncDecls.
//   - End-of-line comments attached to GenDecls and their Specs.
//
// Within any affected comment group, build tags, //go:<directive> lines (e.g., //go:build, //go:generate, //go:embed), cgo directives (// #cgo), nolint directives,
// and “generated” markers are preserved. Comments not attached to those nodes (ex: inside function bodies) are left intact.
//
// In var/const/type blocks, only the matching spec loses its comments; the declaration’s Doc is removed only when removing all docs or when the declaration contains
// a single spec that matches. For struct and interface types, removing the type’s docs also removes comments from struct fields and interface methods.
//
// It returns true if any comment group was altered or deleted. An error is returned only if formatting the updated AST fails.
func RemoveDocumentationInFile(file *gocode.File, identifiers []string) (bool, error) {
	if file.AST == nil {
		return false, nil
	}

	// Build identifier set for quick lookup. A nil slice means remove docs for all identifiers.
	idSet := map[string]struct{}{}
	removeAll := identifiers == nil
	if !removeAll {
		for _, id := range identifiers {
			idSet[id] = struct{}{}
		}
	}

	shouldRemoveIdent := func(name string) bool {
		if removeAll {
			return true
		}
		_, ok := idSet[name]
		return ok
	}

	// Track removed comment groups
	removedComments := make(map[*ast.CommentGroup]bool)
	modified := false

	// Remove package documentation (only when removing all identifiers) while keeping build flags, nolints, and generated markers
	if shouldRemoveIdent(gocode.PackageIdentifier) {
		// Remove package documentation while keeping build flags, nolints, and generated markers
		if file.AST.Doc != nil {
			newDoc, changed := filterDocGroup(file.AST.Doc)
			if changed {
				modified = true
			}
			if newDoc == nil {
				removedComments[file.AST.Doc] = true
				file.AST.Doc = nil
			} else {
				file.AST.Doc = newDoc
			}
		}
	}

	// Visit all declarations
	for _, decl := range file.AST.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			// Decide whether to remove the GenDecl-level doc (remove all, or 1 spec and the 1 spec includes the identifier (even for multi-name vars))
			removeDeclDoc := false
			if removeAll {
				removeDeclDoc = true
			} else if len(d.Specs) == 1 {
				switch s := d.Specs[0].(type) {
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if shouldRemoveIdent(n.Name) {
							removeDeclDoc = true
							break
						}
					}
				case *ast.TypeSpec:
					if shouldRemoveIdent(s.Name.Name) {
						removeDeclDoc = true
					}
				}
			}

			if removeDeclDoc && d.Doc != nil {
				newDoc, changed := filterDocGroup(d.Doc)
				if changed {
					modified = true
				}
				if newDoc == nil {
					removedComments[d.Doc] = true
					d.Doc = nil
				} else {
					d.Doc = newDoc
				}
			}

			// Process specs within the declaration
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.ValueSpec:
					// Determine whether this spec should have its documentation removed.
					removeSpec := removeAll
					if !removeSpec {
						for _, n := range s.Names {
							if shouldRemoveIdent(n.Name) {
								removeSpec = true
								break
							}
						}
					}

					if removeSpec {
						if s.Doc != nil {
							newDoc, changed := filterDocGroup(s.Doc)
							if changed {
								modified = true
							}
							if newDoc == nil {
								removedComments[s.Doc] = true
								s.Doc = nil
							} else {
								s.Doc = newDoc
							}
						}
						if s.Comment != nil {
							newCom, changed := filterDocGroup(s.Comment)
							if changed {
								modified = true
							}
							if newCom == nil {
								removedComments[s.Comment] = true
								s.Comment = nil
							} else {
								s.Comment = newCom
							}
						}
					}
				case *ast.TypeSpec:
					removeSpec := removeAll || shouldRemoveIdent(s.Name.Name)

					if removeSpec {
						// Remove doc and inline comments for the type
						if s.Doc != nil {
							newDoc, changed := filterDocGroup(s.Doc)
							if changed {
								modified = true
							}
							if newDoc == nil {
								removedComments[s.Doc] = true
								s.Doc = nil
							} else {
								s.Doc = newDoc
							}
						}
						if s.Comment != nil {
							newCom, changed := filterDocGroup(s.Comment)
							if changed {
								modified = true
							}
							if newCom == nil {
								removedComments[s.Comment] = true
								s.Comment = nil
							} else {
								s.Comment = newCom
							}
						}

						// For structs and interfaces, also remove docs from their fields/methods
						if structType, ok := s.Type.(*ast.StructType); ok && structType.Fields != nil {
							removeFieldDocs(structType.Fields, removedComments, &modified)
						}
						if interfaceType, ok := s.Type.(*ast.InterfaceType); ok && interfaceType.Methods != nil {
							removeFieldDocs(interfaceType.Methods, removedComments, &modified)
						}
					}
				}
			}

		case *ast.FuncDecl:
			if d.Doc != nil && shouldRemoveIdent(gocode.FuncIdentifierFromDecl(d, file.FileSet)) {
				newDoc, changed := filterDocGroup(d.Doc)
				if changed {
					modified = true
				}
				if newDoc == nil {
					removedComments[d.Doc] = true
					d.Doc = nil
				} else {
					d.Doc = newDoc
				}
			}
		}
	}

	if modified {
		// Filter out removed comments from file.Comments
		var keptComments []*ast.CommentGroup
		for _, cg := range file.AST.Comments {
			if !removedComments[cg] {
				keptComments = append(keptComments, cg)
			}
		}
		file.AST.Comments = keptComments

		// Re-render the AST to update file.Contents
		var buf bytes.Buffer
		err := format.Node(&buf, file.FileSet, file.AST)
		if err != nil {
			return false, err
		}
		file.Contents = buf.Bytes()
	}

	return modified, nil
}

// removeFieldDocs removes documentation from fields in a struct or interface.
func removeFieldDocs(fields *ast.FieldList, removedComments map[*ast.CommentGroup]bool, modified *bool) {
	for _, field := range fields.List {
		if field.Doc != nil {
			newDoc, changed := filterDocGroup(field.Doc)
			if changed {
				*modified = true
			}
			if newDoc == nil {
				removedComments[field.Doc] = true
				field.Doc = nil
			} else {
				field.Doc = newDoc
			}
		}
		if field.Comment != nil {
			newCom, changed := filterDocGroup(field.Comment)
			if changed {
				*modified = true
			}
			if newCom == nil {
				removedComments[field.Comment] = true
				field.Comment = nil
			} else {
				field.Comment = newCom
			}
		}

		// Handle anonymous struct types
		if field.Type != nil {
			switch t := field.Type.(type) {
			case *ast.StructType:
				if t.Fields != nil {
					removeFieldDocs(t.Fields, removedComments, modified)
				}
			case *ast.InterfaceType:
				if t.Methods != nil {
					removeFieldDocs(t.Methods, removedComments, modified)
				}
			}
		}
	}
}

// filterDocGroup removes all comments from cg that are not generated markers or special comment tags (ex: //go:build; //nolint; //revive:...; etc). It returns the
// new comment group and whether any modification occurred.
func filterDocGroup(cg *ast.CommentGroup) (*ast.CommentGroup, bool) {
	if cg == nil {
		return nil, false
	}
	kept := make([]*ast.Comment, 0, len(cg.List))
	changed := false
	for _, c := range cg.List {
		if shouldPreserveComment(c.Text) {
			kept = append(kept, c)
		} else {
			changed = true
		}
	}
	if len(kept) == 0 {
		return nil, changed
	}
	cg.List = kept
	return cg, changed
}

var lintPrefixes = []string{
	"nolint",  // golangci‑lint: format=//nolint[:<l1>,<l2>]
	"lint:",   // staticcheck: format=//lint:ignore <Check>[,<CheckN>] <reason> or //lint:file-ignore <Check>[,<CheckN>] <reason>
	"#nosec",  // gosec: format=//#nosec or //#nosec G304,G401
	"revive:", // revive: format=//revive:disable:<rule> / //revive:enable:<rule>
}

var genRE = regexp.MustCompile(`(?m)^//\s*Code generated .* DO NOT EDIT\.?`)

// shouldPreserveComment reports whether text belongs to one of the special comment categories that must not be removed while pruning: build tags (//go:build or
// the legacy // +build form), any //go:<directive> such as //go:generate or //go:embed, cgo directives (// #cgo), nolint / static-analysis directives (//nolint,
// //lint:ignore, //#nosec, //revive:...), and the standard "Code generated ... DO NOT EDIT" marker.
func shouldPreserveComment(text string) bool {
	t := strings.TrimSpace(text)

	for _, prefix := range lintPrefixes {
		if strings.HasPrefix(t, "//"+prefix) {
			return true
		}
	}

	// handle go:build, go:generate, etc. Because more of these can be added in the future (ex: go:embed), just assume that anything with //go: is a directive of some kind.
	if strings.HasPrefix(t, "//go:") {
		return true
	}

	// pre-1.17 build flags:
	if strings.HasPrefix(t, "// +build") {
		return true
	}

	// cgo:
	if strings.HasPrefix(t, "// #cgo") {
		return true
	}

	// code gen marker:
	if genRE.MatchString(t) {
		return true
	}

	return false
}
