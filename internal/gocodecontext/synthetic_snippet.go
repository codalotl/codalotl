package gocodecontext

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"go/token"
)

// statically assert syntheticPackageSnippet is a gocode.Snippet
var _ gocode.Snippet = &syntheticPackageSnippet{}

// syntheticPackageSnippet represents a generated snippet for package documentation when neither a doc.go file nor a package comment is present. If no such documentation
// exists for a package, pkg.GetSnippet(gocode.PackageIdentifier) correctly returns nil. However, we still require a snippet for IdentifierGroup.Snippets[gocode.PackageIdentifier],
// which contains a snippet with just "package foo" (potentially in a non-existent doc.go file), so that IdentifierGroup includes a Snippet for all IDs. This "snippet"
// can be provided to an LLM as context, allowing instructions that tell the LLM to "document the package".
type syntheticPackageSnippet struct {
	pkgName string // pkgName is the name of the package for which this synthetic snippet is created
}

// IDs returns the identifier for package-level documentation.
func (s *syntheticPackageSnippet) IDs() []string {
	return []string{gocode.PackageIdentifier}
}

// HasExported always returns true for a package snippet.
func (s *syntheticPackageSnippet) HasExported() bool {
	return true
}

// Test returns false; syntheticPackageSnippet never represents test code.
func (s *syntheticPackageSnippet) Test() bool {
	return false
}

// Bytes returns the source bytes for this synthetic snippet: "package <package name>\n".
func (s *syntheticPackageSnippet) Bytes() []byte {
	return []byte("package " + s.pkgName + "\n")
}

// PublicSnippet returns the source bytes for the synthetic snippet; always succeeds.
func (s *syntheticPackageSnippet) PublicSnippet() ([]byte, error) {
	return s.Bytes(), nil
}

// FullBytes returns the full bytes of the synthetic package snippet, which is identical to Bytes.
func (s *syntheticPackageSnippet) FullBytes() []byte {
	return s.Bytes()
}

// Docs returns nil; syntheticPackageSnippet intentionally provides no documentation.
func (s *syntheticPackageSnippet) Docs() []gocode.IdentifierDocumentation {
	return nil
}

// MissingDocs reports that package-level documentation is missing by returning an IdentifierDocumentation for the package identifier.
func (s *syntheticPackageSnippet) MissingDocs() []gocode.IdentifierDocumentation {
	return []gocode.IdentifierDocumentation{{Identifier: gocode.PackageIdentifier}}
}

// Position returns a token.Position indicating a position in "doc.go" for the synthetic snippet.
func (s *syntheticPackageSnippet) Position() token.Position {
	return token.Position{Filename: "doc.go"}
}
