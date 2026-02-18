# gocodecontext

This package creates bundles of context for LLMs to understand Go code. It does this with these main mechanisms:
1. Per-identifier context: get intra-package context for one or more identifiers in a package.
2. Package documentation: get public API with docs (possibly limited to specific identifiers); get all identifiers (incl. private), with or without docs.
3. Identifier usage: get usage information for an identifier (cross-package by default; optionally include intra-package).
4. Package list and module info: list and search through available packages; get module info, like dep modules.

## Per-identifier context

Given a list of package-level identifiers in a package (functions/methods, types, vars/consts), we can create a context (a string) to send to the LLM that offers the LLM a strong understanding of the identifiers.
- If an identifier X is in the list, then X's full source code will be in the context (ex: the full function).
- If an identifier X's code **depends on** (i.e., calls, uses, references) another identifier Y, then Y will be in the context.
    - If Y is a documented function or in another package, then only the docs+signature will be in the context.
    - Otherwise, all of Y will be in the context.
- If an identifier Z is **depended on** (i.e., called by, used by, referenced by) X, then Z will be in the context (full bytes, even if documented).

To do this, we use an `IdentifierGroup`. The `IdentifierGroup` is a bundle of identifiers. Usually, there's only one identifier in an `IdentifierGroup`, but identifiers are grouped together if:
- They occur in the same block (ex: same var block).
- They are in a SCC.

Identifiers are grouped because `gocode.Snippet`s are the things we add to contexts, and because once SCCs go into groups, the `IdentifierGroup`s form a DAG, making things easier.

### Usage

Basic usage:

```go
groups, err := Groups(mod, pkg, GroupOptions{IncludePackageDocs: true, IncludeExternalDeps: true, IncludeTestFiles: false}) // Get []*IdentifierGroup for entire package
targets := FilterGroupsForIdentifiers(groups, ids) // Filter the groups by ids (ex: []string{"myFunc", "myVar", "*myType.myMethod"})
ctx := NewContext(targets) // Make a context
fmt.Println("Token Cost: ", ctx.Cost())

// Get code to send to LLM. Example output (the only IdentifierGroup is a var, which has a direct dependency on a function):
//
//	// code.go:
//
//	// myFunc does...
//	func myFunc() int
//
//	// other.go:
//
//	// myVar is...
//	var myVar = myFunc()
codeContext := ctx.Code()

result := sendToLLM(somePrompt, codeContext) // Send codeContext to LLM
```

Additionally, we can incrementally grow a context under a budget:

```go
ctx := NewContext(initialTargets)
for {
    possibleNextTarget := getNextTarget()
    if possibleNextTarget == nil {
        break
    }
    if ctx.AdditionalCostForGroup(possibleNextTarget) + ctx.Cost() < budget {
         ctx.AddGroup(possibleNextTarget)
    } else {
        break // alternatively, try another target that is smaller
    }
}
```

Finally, if we need to do an operation for each of K identifiers (ex: find bugs in each identifier), use `NewContextsForIdentifiers`:

```go
// Returns a map of context to ids that ctx describes.
// This lets us reduce the number of contexts by recognizing when a context fully describes multiple IDs.
ctxMap := NewContextsForIdentifiers(targets, ids)
```

## Package documentation

During their exploration phase, agent-based LLMs typically `ls` directories and `read` a few .go files in it, then use some form of `grep` to find related code that they're curious about. Instead, we can provide tools for them to get a 'lay of the land' much more quickly:


```go {api exact_docs}
// PublicPackageDocumentation returns a godoc-like documentation string for the package:
//   - Grouped by file (with file comment markers, ex: `// user.go:`).
//   - Each file only has public snippets (no imports; no package-private vars/etc).
//   - Snippets are sorted by the order they appear in the actual file.
//   - Functions only have docs+signatures (even if not documented).
//   - Blocks and structs with unexported elements have those elements elided.
//   - No "floating" comments.
//
// If identifiers are present, documentation is limited to those identifiers:
//   - If any identifier is a type, we also include all public methods on that type.
//   - Most identifiers are just their name. Methods are identified like "*SomePtrType.SomeMethod" or "SomeType.SomeMethod".
//
// Returns an error if pkg is a test package.
func PublicPackageDocumentation(pkg *gocode.Package, identifiers ...string) (string, error)
```

```go {api exact_docs}
// InternalPackageSignatures returns a string for the package:
//   - Grouped by file (with file comment markers, ex: `// user.go:`).
//   - Each file has public and private snippets; includes imports; includes "floating" comments if includeDocs.
//   - Snippets are sorted by the order they appear in the actual file.
//   - All functions/methods are just signatures (no bodies).
//
// If tests, we only do test files. Otherwise, no test files are included. If pkg is a _test package, tests MUST be true.
//
// If includeDocs, all documentation comments are included. Otherwise, all comments are stripped (including floaters).
func InternalPackageSignatures(pkg *gocode.Package, tests bool, includeDocs bool) (string, error)
```

## Identifier usage

```go {api exact_docs}
type IdentifierUsageRef struct {
	ImportPath       string // using package's import path
	AbsFilePath      string // using file's absolute path
	Line             int    // line (1 based) where the usage occurs
	Column           int    // column (1 based) where the usage occurs
	FullLine         string // the full line in the file that uses the identifier (including \n if it exists)
	SnippetFullBytes string // the full bytes of the gocode Snippet that uses identifier
}
```

```go {api exact_docs}
// IdentifierUsage returns usages of identifier as defined in packageAbsDir (i.e., the abs dir of a package).
//
// By default (includeIntraPackageUsages=false), none of the returned usages will be from within the defining package itself. All usages will be from other packages
// that import the defining package. Usages from packageAbsDir's own _test package will not be included.
//
// If includeIntraPackageUsages=true, usages within the defining package (same import path) will also be returned. Usages from the defining package's own _test package
// are still excluded.
//
// The second return value is a string representation of these references, suitable for an LLM:
//   - It will include all references in some manner.
//   - Some references may include the SnippetFullBytes.
//   - Specifics are an implementation detail. Callers should just pass this opaque blob to an LLM.
//
// If packageAbsDir is invalid, or identifier is not defined in packageAbsDir, an error will be returned.
func IdentifierUsage(packageAbsDir, identifier string, includeIntraPackageUsages bool) ([]IdentifierUsageRef, string, error)
```

Even though `IdentifierUsage`'s string return parameter must be **documented** as being opaque, here is the format to use for now:

```txt
--- References ---

codeai/cmd/codagent/main.go
247:	pkgModeInfo, err := initialcontext.Create(pkg)

codeai/initialcontext/initial_context_test.go
24:	got, err := Create(pkg)

codeai/initialcontext/initial_context.go
21:func Create(pkg *gocode.Package) (string, error) {

--- A handful of examples of usage ---

codeai/cmd/codagent/main.go
func createContext(pkg *gocode.Package) (string, error) {
    pkgModeInfo, err := initialcontext.Create(pkg)
    if err != nil {
        return "", err
    }
    return pkgModeInfo + supplementalContext()
}
```

- First, list all references in a manner similar to `rg`.
- Then, list a handful of snippets of full context (often functions). Criteria for choosing snippets:
   - Only list snippets smaller than `maxSnippetLines` (currently 150).
   - Choose up to `maxSnippetContexts` snippets (currently 2), preferring the shortest snippets.
- If there are no snippets, don't display the corresponding banner.

## Package lists and module info

```go {api exact_docs}
// PackageList returns a list of packages available in the current module. It identifies the go.mod file by starting at absDir and walking up until it finds a go.mod
// file.
//
// main and _test packages are included. _test packages listed by their "import path" - if a directory contains a non-test and a test package, the path is only listed
// once.
//
// If search is given, it filters the results by interpreting it as a Go regexp.
//
// If !includeDepPackages, it only includes packages defined in this module. Otherwise, it includes packages in **direct** module dependencies (dependency internal
// packages excluded). "Direct module dependencies" means modules listed in the go.mod `require` block(s) that are NOT annotated with `// indirect` (go.sum is ignored).
//
// It returns a slice of sorted packages; a string that can be dropped in as context to an LLM; an error, if any.
//
// The LLM context string is intentionally opaque (callers should not rely on parsing it; they should directly drop it into an LLM). That said, conceptually, it
// might look like:
//
//	These packages are defined in the current module (github.com/someuser/myproj):
//	- github.com/someuser/myproj/foo
//	- github.com/someuser/myproj/bar
//	- github.com/someuser/myproj/internal/api
//	- github.com/someuser/myproj/internal/gen/... (232 packages omitted; expand with search="^github\\.com/someuser/myproj/internal/gen(/|$)")
//	- github.com/someuser/myproj/internal/schema
//
//	Defined in golang.org/x/mod:
//	- golang.org/x/mod/gosumcheck
//	- golang.org/x/mod/modfile
//
// The returned slice will never be truncated. However, if there are too many packages, the LLM context may collapse large nodes with a message indicating how to
// access them (see example above).
//
// IDEA: in the future, we may want to provide a short description of each package, and have that be searchable.
func PackageList(absDir, search string, includeDepPackages bool) ([]string, string, error)
```

```go {api exact_docs}
// ModuleInfo returns information about the current Go module. It identifies the go.mod file by starting at absDir and walking up until it finds a go.mod file.
//
// It returns an LLM context string that can be directly dropped into an LLM, and an error, if any.
//
// The LLM context string is intentionally opaque (callers should not rely on parsing it; they should directly drop it into an LLM). That said, conceptually, it
// might look like:
//
//	module github.com/someuser/myproj
//
//	go 1.24
//
// NOTES:
//   - For now, this just returns the go.mod file itself.
//   - Need to do more research about how big these can get. We may want to implement things like limiting deps to direct dependencies; stripping comments; being
//     more concise; module search.
func ModuleInfo(absDir string) (string, error)
```
