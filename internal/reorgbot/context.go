package reorgbot

import (
	"bytes"
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gograph"
	"sort"
	"strings"
)

// codeContextForPackage returns context we can send to the LLM and a map of canonical ID -> Snippet.
//
// All ids in the context are present in the returned map. Some of these IDs are different than the standard gocode IDs -- they are "compound IDs" for a whole var/const/type
// block, using ambiguous ID format.
func codeContextForPackage(pkg *gocode.Package, ids []string, isTests bool) (string, map[string]gocode.Snippet) {

	var b strings.Builder

	if isTests {
		b.WriteString("// NON TEST FILES:\n")
		nonTestPkg, err := pkg.Module.LoadPackageByRelativeDir(pkg.RelativeDir)
		if err != nil {
			panic(fmt.Errorf("unexpectedly couldn't find non-test package: %v", err))
		}
		for _, f := range nonTestPkg.Files {
			if !f.IsTest {
				b.WriteString("// ")
				b.WriteString(f.FileName)
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}

	canonicalToSnippet := make(map[string]gocode.Snippet)
	// Build an intra-package graph once to compute function call relationships.
	// If graph construction fails, we'll proceed without call information.
	graph, _ := gograph.NewGoGraph(pkg)
	snippetsByFile := pkg.SnippetsByFile(ids)

	for fileName, snippets := range snippetsByFile {

		b.WriteString(fmt.Sprintf("// File: %s\n\n", fileName))

		for _, s := range snippets {

			fullBytes := s.FullBytes()

			id := canonicalSnippetID(s)
			b.WriteString(fmt.Sprintf("// id: %s\n", id))
			canonicalToSnippet[id] = s

			switch s := s.(type) {
			case *gocode.FuncSnippet:
				b.WriteString(fmt.Sprintf("// lines of code: %d\n", bytes.Count(fullBytes, []byte("\n"))))
				// Intra-package calls made by this function
				if graph != nil {
					calls := intraPkgFuncCalls(pkg, graph, s.Identifier)
					if len(calls) > 0 {
						b.WriteString(fmt.Sprintf("// calls: %s\n", strings.Join(calls, ", ")))
					}
					callers := intraPkgFuncCallers(pkg, graph, s.Identifier)
					if len(callers) > 0 {
						b.WriteString(fmt.Sprintf("// called by: %s\n", strings.Join(callers, ", ")))
					}
				}
				b.WriteString(fmt.Sprintf("%s\n", s.Sig))
			case *gocode.ValueSnippet:
				if graph != nil {
					usedBy := intraPkgUsedBy(pkg, graph, s.Identifiers)
					if len(usedBy) > 0 {
						b.WriteString(fmt.Sprintf("// used by: %s\n", strings.Join(usedBy, ", ")))
					}
				}
				_, _ = b.Write(fullBytes)
				b.WriteString("\n")
			case *gocode.TypeSnippet:
				if graph != nil {
					usedBy := intraPkgUsedBy(pkg, graph, s.Identifiers)
					if len(usedBy) > 0 {
						b.WriteString(fmt.Sprintf("// used by: %s\n", strings.Join(usedBy, ", ")))
					}
				}
				_, _ = b.Write(fullBytes)
				b.WriteString("\n")
			case *gocode.PackageDocSnippet:
				_, _ = b.Write(fullBytes)
				b.WriteString("\n")
			}

			b.WriteString("\n")

		}
	}

	return b.String(), canonicalToSnippet
}

// intraPkgFuncCalls returns the sorted list of intra-package function/method identifiers called by fromID.
func intraPkgFuncCalls(pkg *gocode.Package, graph *gograph.Graph, fromID string) []string {
	deps := graph.IdentifiersFrom(fromID)
	if len(deps) == 0 {
		return nil
	}
	var funcCalls []string
	for _, dep := range deps {
		if snip := pkg.GetSnippet(dep); snip != nil {
			if _, ok := snip.(*gocode.FuncSnippet); ok {
				funcCalls = append(funcCalls, dep)
			}
		}
	}
	if len(funcCalls) == 0 {
		return nil
	}
	sort.Strings(funcCalls)
	return funcCalls
}

// intraPkgFuncCallers returns the sorted list of intra-package function/method identifiers that call toID.
func intraPkgFuncCallers(pkg *gocode.Package, graph *gograph.Graph, toID string) []string {
	froms := graph.IdentifiersTo(toID)
	if len(froms) == 0 {
		return nil
	}
	var callers []string
	for _, from := range froms {
		if snip := pkg.GetSnippet(from); snip != nil {
			if _, ok := snip.(*gocode.FuncSnippet); ok {
				callers = append(callers, from)
			}
		}
	}
	if len(callers) == 0 {
		return nil
	}
	sort.Strings(callers)
	return callers
}

// intraPkgUsedBy returns the sorted union of intra-package function/method identifiers that reference any of ids.
func intraPkgUsedBy(pkg *gocode.Package, graph *gograph.Graph, ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	set := make(map[string]struct{})
	for _, id := range ids {
		callers := graph.IdentifiersTo(id)
		for _, from := range callers {
			if snip := pkg.GetSnippet(from); snip != nil {
				if _, ok := snip.(*gocode.FuncSnippet); ok {
					set[from] = struct{}{}
				}
			}
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// codeContextForFile returns the contents of a single file with an "// id: <id>" comment inserted immediately before each non-package snippet (functions, types,
// values). The returned map contains canonical id -> snippet for all non-package snippets in the file. Package documentation is emitted as-is (no id comment) and
// is never included in the map.
func codeContextForFile(pkg *gocode.Package, file *gocode.File) (string, map[string]gocode.Snippet) {
	// Ensure snippets are available for this file (SnippetsByFile loads them lazily)
	snippets := pkg.SnippetsByFile(nil)[file.FileName]

	// Build a list of insertion points (snippet start offsets) paired with the id comment to insert.
	type insertion struct {
		offset int
		text   string
	}
	var insertions []insertion
	idToSnippet := make(map[string]gocode.Snippet)
	for _, s := range snippets {
		if _, isPkgDoc := s.(*gocode.PackageDocSnippet); isPkgDoc {
			// Do not add an id for package documentation and do not include it in ids
			continue
		}
		start := s.Position().Offset
		if start <= 0 {
			continue
		}
		id := canonicalSnippetID(s)
		idToSnippet[id] = s
		insertions = append(insertions, insertion{offset: start, text: "// id: " + id + "\n"})
	}

	// If there are no snippets (or only package docs), return original contents.
	if len(insertions) == 0 {
		return string(file.Contents), idToSnippet
	}

	// Sort insertions by offset ascending to process in source order.
	sort.Slice(insertions, func(i, j int) bool { return insertions[i].offset < insertions[j].offset })

	// Compose new file by copying original contents and inserting id comments at the snippet starts.
	var b strings.Builder
	cursor := 0
	for _, ins := range insertions {
		if ins.offset > len(file.Contents) {
			// Clamp to file end if something is off
			ins.offset = len(file.Contents)
		}
		if ins.offset < cursor {
			// Should not happen; skip to maintain monotonicity
			continue
		}
		b.Write(file.Contents[cursor:ins.offset])
		b.WriteString(ins.text)
		cursor = ins.offset
	}
	// Append remaining content
	if cursor < len(file.Contents) {
		b.Write(file.Contents[cursor:])
	}

	return b.String(), idToSnippet
}

// canonicalSnippetID returns the canonical identifier for the given snippet.
func canonicalSnippetID(snippet gocode.Snippet) string {
	switch s := snippet.(type) {
	case *gocode.FuncSnippet:
		return s.Identifier
	case *gocode.ValueSnippet:
		if len(s.Identifiers) == 1 {
			return s.Identifiers[0]
		}
		pos := s.Position()
		return gocode.AnonymousIdentifier(pos.Filename, pos.Line, pos.Column)
	case *gocode.TypeSnippet:
		if len(s.Identifiers) == 1 {
			return s.Identifiers[0]
		}
		pos := s.Position()
		return gocode.AnonymousIdentifier(pos.Filename, pos.Line, pos.Column)
	case *gocode.PackageDocSnippet:
		return s.Identifier
	default:
		panic("unknown snippet type")
	}
}
