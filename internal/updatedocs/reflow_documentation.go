package updatedocs

import (
	"github.com/codalotl/codalotl/internal/gocode"
)

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
//  2. Convert fields/specs to EOL vs. Doc comments based on whether they can fit, and based on maximizing uniformity (ex: if everything is a .Doc comment except for one field, make
//     that field a .Doc comment as well).
//  3. Adjust newline whitespace within struct types and value blocks (ex: a .Doc comment should have a blank line above it, not code).
//
// options is only used for ReflowMaxWidth and ReflowTabWidth - other fields are unused. It returns a reloaded Package if anything was modified (just like UpdateDocumentation), any
// identifiers that were NOT successfully reflowed, and any hard error (ex: I/O error). Identifiers that were not successfully reflowed will NOT cause this function to return an error.
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
