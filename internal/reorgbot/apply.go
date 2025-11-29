package reorgbot

import (
	"github.com/codalotl/codalotl/internal/goclitools"
	"github.com/codalotl/codalotl/internal/gocode"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// applyReorganization re-writes the files of pkg according to newOrganization. It is dangerous and destructive! It may delete files, create files, and completely re-write files
// to have new contents. The new set of files that exist should have the same snippets as the old set of files - just in a new order, located in possibly different files.
//
// newOrganization is a map of target filename to a sorted slice of canonical snippet ids, meaning that after applyReorganization is done, that filename will have those snippets in
// that order. idToSnippet is a map of canonical snippet ID to gocodeSnippet. It MUST be the case that orgIsValid(newOrganization, idToSnippet, onlyTests) before calling this function.
//
// If onlyTests, all files and snippets are test snippets (either in a _test package, or the main package). Otherwise, they are all non-test files.
//
// After the new snippets are put in place, the file imports and formatting will be fixed by attempting goimports, falling back to gofmt.
//
// An error is returned for hard errors like I/O errors.
func applyReorganization(pkg *gocode.Package, newOrganization map[string][]string, idToSnippet map[string]gocode.Snippet, onlyTests bool) error {
	// Validate that all referenced ids exist in idToSnippet (orgIsValid should already guarantee this).
	for _, ids := range newOrganization {
		for _, id := range ids {
			if _, ok := idToSnippet[id]; !ok {
				return fmt.Errorf("id %q not found in idToSnippet", id)
			}
		}
	}

	// Build set of destination file names (deterministic order for stable behavior).
	destFileNames := make([]string, 0, len(newOrganization))
	for fn := range newOrganization {
		destFileNames = append(destFileNames, fn)
	}
	sort.Strings(destFileNames)

	// Build a mapping from canonical snippet id -> unattached comments that originally
	// preceded that snippet (in original files). These comments should travel with the
	// snippet and be emitted immediately before it in the destination file.
	idToPreComments := make(map[string][]string)

	// Also collect unattached comments that do not have a following snippet in their
	// original file (Next == nil). These should be written back to their original
	// filename after we finish writing the new organization, even if that filename is
	// not present in newOrganization.
	orphanCommentsByFile := make(map[string][]string)

	for _, uc := range pkg.UnattachedComments {
		// Respect test vs non-test phase. For comments with a Next snippet, use Next.Test().
		// For orphan comments (Next == nil), infer from originating filename.
		if uc.Next != nil {
			s := uc.Next
			// Filter by phase: onlyTests should match the snippet's Test() flag.
			if s.Test() != onlyTests {
				continue
			}
			id := canonicalSnippetID(s)
			idToPreComments[id] = append(idToPreComments[id], string(ensureSingleTrailingNewline([]byte(uc.Comment))))
			continue
		}

		// Orphan comment: keep only comments that belong to files in this phase.
		isTestFile := strings.HasSuffix(uc.FileName, "_test.go")
		if isTestFile != onlyTests {
			continue
		}
		orphanCommentsByFile[uc.FileName] = append(orphanCommentsByFile[uc.FileName], uc.Comment)
	}

	// Plan and manage imports for this reorganization phase.
	planner := newImportPlanner(pkg, idToSnippet, onlyTests)

	// Construct and write each destination file.
	pkgDir := pkg.AbsolutePath()
	packageName := pkg.Name // for test packages, this will already include _test

	wroteFiles := make(map[string]struct{}, len(destFileNames))

	// Imports are handled by planner; nothing to pre-compute here.

	for _, fileName := range destFileNames {
		ids := newOrganization[fileName]

		var buf bytes.Buffer

		// Detect package doc snippets; ensure at most one.
		var pkgDocSnippet *gocode.PackageDocSnippet
		for _, id := range ids {
			s := idToSnippet[id]
			if s == nil {
				continue
			}
			if pds, ok := s.(*gocode.PackageDocSnippet); ok {
				if pkgDocSnippet != nil {
					return fmt.Errorf("multiple package doc snippets assigned to %s", fileName)
				}
				pkgDocSnippet = pds
			}
		}

		// Write header: either package doc (includes package clause) or a plain package clause.
		if pkgDocSnippet != nil {
			// Emit any unattached comments that originally preceded this package doc snippet.
			pkgDocID := canonicalSnippetID(pkgDocSnippet)
			if pres := idToPreComments[pkgDocID]; len(pres) > 0 {
				for _, c := range pres {
					buf.WriteString(c)
				}
				buf.WriteString("\n")
			}
			// Ensure it ends with a single trailing newline.
			header := ensureSingleTrailingNewline(pkgDocSnippet.Snippet)
			buf.Write(header)
			buf.WriteString("\n")
		} else {
			// Plain package clause.
			buf.WriteString("package ")
			buf.WriteString(packageName)
			buf.WriteString("\n\n")
		}

		// Emit import sections using the planner.
		planner.writeImports(&buf, fileName, ids, idToSnippet, idToPreComments)

		// Append non-package snippets in the specified order. Before each snippet, emit any
		// unattached comments that originally preceded that snippet.
		for _, id := range ids {
			s := idToSnippet[id]
			if s == nil {
				continue
			}
			if _, isPkgDoc := s.(*gocode.PackageDocSnippet); isPkgDoc {
				continue
			}
			// Emit pre-comments that followed this snippet in the source files.
			if pres := idToPreComments[id]; len(pres) > 0 {
				for _, c := range pres {
					// c already ensured to end with one newline
					buf.WriteString(c)
				}
				// Blank line between comments and the snippet for readability.
				buf.WriteString("\n")
			}

			snippetBytes := s.FullBytes()
			// Ensure snippet ends with exactly one newline, and add a blank line between snippets.
			snippetBytes = ensureSingleTrailingNewline(snippetBytes)
			buf.Write(snippetBytes)
			buf.WriteString("\n")
		}

		// Persist file contents.
		absPath := filepath.Join(pkgDir, fileName)
		if err := os.WriteFile(absPath, buf.Bytes(), 0644); err != nil {
			return fmt.Errorf("write %s: %w", absPath, err)
		}
		wroteFiles[fileName] = struct{}{}
	}

	// Write back orphan unattached comments to their original files. If a file was already
	// written above (present in wroteFiles), append these comments at the end of the file,
	// preserving a blank line separation. Otherwise, create the file with just the package
	// clause and the comments.
	for fileName, comments := range orphanCommentsByFile {
		if len(comments) == 0 {
			continue
		}
		absPath := filepath.Join(pkgDir, fileName)
		if _, exists := wroteFiles[fileName]; exists {
			// Append to existing content
			existing, err := os.ReadFile(absPath)
			if err != nil {
				return fmt.Errorf("read %s: %w", absPath, err)
			}
			var b bytes.Buffer
			b.Write(existing)
			// Ensure there is exactly one blank line before appending comments
			trimmed := strings.TrimRight(b.String(), "\n")
			b.Reset()
			b.WriteString(trimmed)
			b.WriteString("\n\n")
			for _, c := range comments {
				b.Write(ensureSingleTrailingNewline([]byte(c)))
			}
			// Ensure trailing newline
			b.WriteString("\n")
			if err := os.WriteFile(absPath, b.Bytes(), 0644); err != nil {
				return fmt.Errorf("write %s: %w", absPath, err)
			}
		} else {
			var b bytes.Buffer
			// Header: plain package clause
			b.WriteString("package ")
			b.WriteString(packageName)
			b.WriteString("\n\n")
			for _, c := range comments {
				b.Write(ensureSingleTrailingNewline([]byte(c)))
			}
			// Ensure trailing newline
			b.WriteString("\n")
			if err := os.WriteFile(absPath, b.Bytes(), 0644); err != nil {
				return fmt.Errorf("write %s: %w", absPath, err)
			}
			wroteFiles[fileName] = struct{}{}
		}
	}

	// Delete old files of this kind (test vs non-test) that are not present in the new organization
	// or in the orphan set we just wrote.
	// Only operate within this package's file set (does not touch black-box tests if pkg is the main pkg).
	for existingName, f := range pkg.Files {
		if f == nil {
			continue
		}
		isTestFile := strings.HasSuffix(existingName, "_test.go")
		if isTestFile != onlyTests {
			continue
		}
		if _, keep := wroteFiles[existingName]; keep {
			continue
		}
		// Remove file from disk; ignore if it doesn't exist.
		abs := filepath.Join(pkgDir, existingName)
		_ = os.Remove(abs)
	}

	// Format and organize imports in the package directory.
	_, err := goclitools.FixImports(pkgDir)
	if err != nil {
		return err
	}

	return nil
}

// ensureSingleTrailingNewline ensures b ends with exactly one trailing '\n'.
func ensureSingleTrailingNewline(b []byte) []byte {
	// Trim any number of trailing newlines, then add exactly one.
	s := string(b)
	s = strings.TrimRight(s, "\n")
	return append([]byte(s), '\n')
}
