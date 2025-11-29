package gopackagediff

import (
	"fmt"
	"sort"

	"github.com/codalotl/codalotl/internal/diff"
	"github.com/codalotl/codalotl/internal/gocode"
)

// Change represents a change in a gocode.Snippet from the old package version to the new package version.
//
// If snippets are added or deleted (ex: adding an entirely new func), one of OldIdentifiers or NewIdentifiers will be nil, one of NewCode or OldCode will be "", and one snippet will
// be nil.
//
// Invariants:
//   - If OldSnippet != nil && NewSnippet != nil, then same snippet kind in both versions (ex: changing an id from func to var cannot be done in a single Change).
//   - OldIdentifiers == OldSnippet&.IDs() and NewIdentifiers == NewSnippet&.IDs() (&. is access-if-not-nil-otherwise-nil).
//   - At least one of OldIdentifiers or NewIdentifiers is non-nil/non-empty. Same for OldCode/NewCode, and OldSnippet/NewSnippet.
//   - OldCode != NewCode.
type Change struct {
	IdentifiersChanged bool // If the identifiers for a snippet are the same in both the new and old package, IdentifiersChanged == false and OldIdentifiers == NewIdentifiers.
	OldIdentifiers     []string
	NewIdentifiers     []string
	OldCode            string // old snippet's code (full function body presence is based on Diff's excludeFuncBody)
	NewCode            string // new snippet's code
	OldSnippet         gocode.Snippet
	NewSnippet         gocode.Snippet
}

// ColorizedDiff returns a character-level colored diff using diffmatchpatch's pretty text representation.
func (c Change) ColorizedDiff() string {
	d := diff.DiffText(c.OldCode, c.NewCode)

	filename := ""
	if c.OldSnippet != nil {
		filename = c.OldSnippet.Position().Filename
	} else if c.NewSnippet != nil {
		filename = c.NewSnippet.Position().Filename
	}

	return d.RenderPretty(filename, filename, 20)
}

// IDSet returns a set of all IDs affected by this change (OldIdentifiers union NewIdentifiers).
func (c Change) IDSet() map[string]struct{} {
	set := make(map[string]struct{})
	for _, id := range c.OldIdentifiers {
		set[id] = struct{}{}
	}
	for _, id := range c.NewIdentifiers {
		set[id] = struct{}{}
	}
	return set
}

// IDs returns all IDs affected by this change (OldIdentifiers union NewIdentifiers), sorted for determinism.
func (c Change) IDs() []string {
	set := c.IDSet()
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Diff computes the changes from origPkg to newPkg.
//   - If identifiers is nil, all identifiers are considered. Otherwise, only snippets for the listed identifiers are considered. An identifier may exist only in origPkg or only in newPkg.
//     If an identifier is in neither, it is ignored.
//   - If files is nil, all files are considered. Otherwise, only identifiers in the listed files (union of old and new files' identifiers) are considered. If a file does not exist,
//     it is ignored. Each element of files is just a bare file name.
//   - If both identifiers and files are non-nil, the intersection is used.
//   - If excludeFuncBody is true, the diff covers only non-function-body code and documentation. OldCode and NewCode will not include function bodies, and differences inside function
//     bodies are ignored.
//
// It returns the changes and any error.
//   - An identifier can be found in at most one of changes' OldIdentifiers (OldIdentifiers are disjoint). Similarly, NewIdentifiers are disjoint.
//   - Similarly, an OldSnippet can be in at most one Change; a NewSnippet can be in at most one Change.
//   - Otherwise, the specific grouping of changes is an implementation detail (ex: if two lone consts are merged into one block snippet, the change could be represented by one Change
//     gaining an identifier and another losing an identifier, or by both original snippets being deleted and a third created with both identifiers).
func Diff(origPkg *gocode.Package, newPkg *gocode.Package, identifiers []string, files []string, excludeFuncBody bool) ([]*Change, error) {
	// Normalize nil packages:
	if origPkg == nil || newPkg == nil {
		return nil, fmt.Errorf("nil package")
	}

	// Build identifier set to consider:
	considered := make(map[string]struct{})
	if identifiers == nil {
		for _, id := range origPkg.Identifiers(true) {
			considered[id] = struct{}{}
		}
		for _, id := range newPkg.Identifiers(true) {
			considered[id] = struct{}{}
		}
	} else {
		for _, id := range identifiers {
			considered[id] = struct{}{}
		}
	}

	// Optional file filter:
	if files != nil {
		idsInFiles := getIDsInFiles(origPkg, newPkg, files)
		considered = intersect(considered, idsInFiles)
	}

	if len(considered) == 0 {
		return nil, nil
	}

	// Group considered identifiers by snippet per package.
	type idSet = map[string]struct{}
	oldSnippetToIds := make(map[gocode.Snippet]idSet)
	newSnippetToIds := make(map[gocode.Snippet]idSet)

	for id := range considered {
		if sn := origPkg.GetSnippet(id); sn != nil {
			if oldSnippetToIds[sn] == nil {
				oldSnippetToIds[sn] = make(idSet)
			}
			oldSnippetToIds[sn][id] = struct{}{}
		}
		if sn := newPkg.GetSnippet(id); sn != nil {
			if newSnippetToIds[sn] == nil {
				newSnippetToIds[sn] = make(idSet)
			}
			newSnippetToIds[sn][id] = struct{}{}
		}
	}

	// Build weighted edges between old and new snippets based on shared identifiers; only connect same-kind snippets.
	type edge struct {
		old gocode.Snippet
		neu gocode.Snippet
		w   int
	}
	var edges []edge

	for oldSn, oldIds := range oldSnippetToIds {
		for id := range oldIds {
			if newSn := newPkg.GetSnippet(id); newSn != nil {
				if snippetKind(oldSn) != snippetKind(newSn) {
					continue
				}
				// Find or create edge (oldSn,newSn)
				added := false
				for i := range edges {
					if edges[i].old == oldSn && edges[i].neu == newSn {
						edges[i].w++
						added = true
						break
					}
				}
				if !added {
					edges = append(edges, edge{old: oldSn, neu: newSn, w: 1})
				}
			}
		}
	}

	// Greedy maximum weight matching: sort edges by descending weight and match unique pairs.
	sort.SliceStable(edges, func(i, j int) bool { return edges[i].w > edges[j].w })
	matchedOld := make(map[gocode.Snippet]gocode.Snippet)
	matchedNew := make(map[gocode.Snippet]gocode.Snippet)
	for _, e := range edges {
		if _, ok := matchedOld[e.old]; ok {
			continue
		}
		if _, ok := matchedNew[e.neu]; ok {
			continue
		}
		matchedOld[e.old] = e.neu
		matchedNew[e.neu] = e.old
	}

	// Assemble changes: matched pairs that differ, plus unmatched additions and deletions.
	var changes []*Change

	// Matched pairs:
	for oldSn, newSn := range matchedOld {
		oldCode := codeForSnippet(oldSn, excludeFuncBody)
		newCode := codeForSnippet(newSn, excludeFuncBody)

		oldIDs := oldSn.IDs()
		newIDs := newSn.IDs()

		idsChanged := !stringSlicesEqual(oldIDs, newIDs)
		codeChanged := oldCode != newCode
		if idsChanged || codeChanged {
			changes = append(changes, &Change{
				IdentifiersChanged: idsChanged,
				OldIdentifiers:     cloneStrings(oldIDs),
				NewIdentifiers:     cloneStrings(newIDs),
				OldCode:            oldCode,
				NewCode:            newCode,
				OldSnippet:         oldSn,
				NewSnippet:         newSn,
			})
		}
	}

	// Deletions (old snippets not matched):
	for oldSn := range oldSnippetToIds {
		if _, ok := matchedOld[oldSn]; ok {
			continue
		}
		changes = append(changes, &Change{
			IdentifiersChanged: true,
			OldIdentifiers:     cloneStrings(oldSn.IDs()),
			NewIdentifiers:     nil,
			OldCode:            codeForSnippet(oldSn, excludeFuncBody),
			NewCode:            "",
			OldSnippet:         oldSn,
			NewSnippet:         nil,
		})
	}

	// Additions (new snippets not matched):
	for newSn := range newSnippetToIds {
		if _, ok := matchedNew[newSn]; ok {
			continue
		}
		changes = append(changes, &Change{
			IdentifiersChanged: true,
			OldIdentifiers:     nil,
			NewIdentifiers:     cloneStrings(newSn.IDs()),
			OldCode:            "",
			NewCode:            codeForSnippet(newSn, excludeFuncBody),
			OldSnippet:         nil,
			NewSnippet:         newSn,
		})
	}

	// Deterministic ordering: sort by a stable key derived from identifiers.
	sort.SliceStable(changes, func(i, j int) bool {
		fi := changeFilename(changes[i])
		fj := changeFilename(changes[j])
		if fi == fj {
			ki := changeSortKey(changes[i])
			kj := changeSortKey(changes[j])
			if ki == kj {
				// Tie-breaker by snippet kind to keep stable order
				return snippetKind(changes[i].OldSnippet) < snippetKind(changes[j].OldSnippet)
			}
			return ki < kj
		}
		return fi < fj
	})

	return changes, nil
}

func getIDsInFiles(oldPkg *gocode.Package, newPkg *gocode.Package, files []string) map[string]struct{} {
	ids := make(map[string]struct{})

	fileSet := make(map[string]struct{})
	for _, f := range files {
		fileSet[f] = struct{}{}
	}

	addIdentsForPkg := func(p *gocode.Package) {
		for _, s := range p.Snippets() {
			if _, ok := fileSet[s.Position().Filename]; ok {
				for _, id := range s.IDs() {
					ids[id] = struct{}{}
				}
			}
		}

	}
	addIdentsForPkg(oldPkg)
	addIdentsForPkg(newPkg)

	return ids
}

func intersect[T comparable](a, b map[T]struct{}) map[T]struct{} {
	ret := make(map[T]struct{})

	for k := range a {
		if _, ok := b[k]; ok {
			ret[k] = struct{}{}
		}
	}

	return ret
}

// snippetKind returns a coarse-grained kind string for the snippet's concrete type.
func snippetKind(s gocode.Snippet) string {
	switch s.(type) {
	case *gocode.FuncSnippet:
		return "func"
	case *gocode.TypeSnippet:
		return "type"
	case *gocode.ValueSnippet:
		return "value"
	case *gocode.PackageDocSnippet:
		return "package"
	default:
		return ""
	}
}

// codeForSnippet returns textual code for the snippet, honoring excludeFuncBody for functions.
func codeForSnippet(s gocode.Snippet, excludeFuncBody bool) string {
	if s == nil {
		return ""
	}
	// For functions, Bytes() is docs+signature, FullBytes() includes body.
	if _, ok := s.(*gocode.FuncSnippet); ok {
		if excludeFuncBody {
			return string(s.Bytes())
		}
		return string(s.FullBytes())
	}
	// For other snippet kinds, Bytes() == FullBytes() for our purposes.
	return string(s.FullBytes())
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// changeSortKey returns a stable lexicographic key for a change based on its identifiers.
func changeSortKey(c *Change) string {
	// Prefer first OldIdentifiers; fallback to NewIdentifiers.
	if len(c.OldIdentifiers) > 0 {
		return c.OldIdentifiers[0]
	}
	if len(c.NewIdentifiers) > 0 {
		return c.NewIdentifiers[0]
	}
	// Fallback: kind prefix to keep groups stable.
	if c.OldSnippet != nil {
		return snippetKind(c.OldSnippet)
	}
	if c.NewSnippet != nil {
		return snippetKind(c.NewSnippet)
	}
	return ""
}

// changeFilename returns the filename associated with this change for sorting/grouping.
func changeFilename(c *Change) string {
	if c == nil {
		return ""
	}
	if c.NewSnippet != nil {
		return c.NewSnippet.Position().Filename
	}
	if c.OldSnippet != nil {
		return c.OldSnippet.Position().Filename
	}
	return ""
}
