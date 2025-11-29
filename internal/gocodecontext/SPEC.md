# gocodecontext

This package creates bundles of context for LLMs to understand Go code. It does this with three main mechanims:
1. Per-identifier context: get intra-package context for one or more identifiers in a package.
2. Package documentation: get public API with docs (possibly limited to specific identifiers); get all identifiers (incl. private), with or without docs.
3. Cross-package usage: get usage information from other packages that use an identifier.

## Per-identifier context

Given a list of package-level identifiers in a package (functions/methods, types, vars/consts), we can create a context (a string) to send to the LLM that offers the LLM a strong understanding of the identifiers.
- If an identifier X is in the list, then X's full source code will be in the context (ex: the full function).
- If an identifier X's code **depends on** (i.e., calls, uses, references) another identifier Y, then Y will be in the context.
    - If Y is a documented function or in another package, then only the docs+signature will be in the context.
    - Otherwise, all of Y will be in the context.
- If an identifier Z is **depended on* (i.e., called by, used by, referenced by) X, then Z will be in the context (full bytes, even if documented).

To do this, we use an `IdentifierGroup`. The `IdentifierGroup` is a bundle of identifiers. Usually, there's only one identifier in an `IdentifierGroup`, but identifiers are grouped together if:
- They occur in the same block (ex: same var block).
- They are in a SCC.

Identifiers are grouped because `gocode.Snippet`s are thing we add to contexts, and because once SCCs go into groups, the `IdentifierGroups` from a DAG, making things easier.

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

During their exploration phase, agent-based LLMs typically `ls` directories and `read` a few .go files in it, then use some form of `grep` to find related code that they're curious about. Intead, we can provide tools for them to get a 'lay of the land' much more quickly:


```go {api exact_docs}
// PublicPackageDocumentation returns a godoc-like documentation string for the package:
//   - Grouped by file (with file comment markers, ex: `// user.go:`).
//   - Each file only has public snippets (no imports; no package-private vars/etc).
//   - Snippets are sorted by the order they appear in the actual file.
//   - Functions only have docs+signatures (even if not documented).
//   - blocks and structs with unexported elements have those elements elided.
//   - No "floating" comments.
//
// If identifiers are present, documentation is limited identifiers:
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

## Cross-package usage

```go {api exact_docs}
type CrossPackageUsage struct {
    ImportPath string // using package's import path
    AbsFilePath string // using file's absolute path
    Line int // line (1 based) where the usage occurs
    Column int // column (1 based) where the usage occurs
    FullLine string // the full line in the file that uses the identifier (including \n if it exists)
    SnippetFullBytes string // the full bytes of the gocode Snippet that uses identifier
}
```

```go {api exact_docs}
// CrossPackageUsage returns references to identifier as defined in packageAbsDir (i.e., the abs dir of a package). None of the references will be from within the packageAbsDir package itself.
// All references will be from other packages that import the package. Usages from packageAbsDir's own _test package will not be included. But other _test packages
// in other dirs will be included.
//
// The second return value is a string representation of these references, suitable for an LLM:
//   - It will include all references in some manner.
//   - Some references may include the SnippetFullBytes.
//   - Specifics are an implementation detail. Callers should just pass this opaque blob to an LLM.
//
// If packageAbsDir is invalid, or identifier is not defined in packageAbsDir, an error will be returned.
func CrossPackageUsage(packageAbsDir, identifier string) ([]CrossPackageUsage, string, error)
```

Even though `CrossPackageUsage`'s string return parameter must be **documented** as being opaque, here is the format to use for now:

```txt
--- References ---

codeai/cmd/codagent/main.go
247:	pkgModeInfo, err := initialcontext.Create(pkg)

codeai/initialcontext/initial_context_test.go
24:	got, err := Create(pkg)

codeai/initialcontext/initial_context.go
21:func Create(pkg *gocode.Package) (string, error) {

--- Select full contexts ---

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
- List up to 3 snippets of full context (often functions). Criteria for chosing snippets:
   - Only list a function if its less than 200 lines (configurable via const).
   - Chose the 3 smallest snippets
- If there's no snippets, don't display the corresponding banner.
