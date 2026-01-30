package gocode

import "sort"

// SnippetsByFile returns snippets grouped by file name. If ids is non-empty, only ids will be present (missing ids are ignored).
// The snippets will be in the same order as they occur on in source.
func (p *Package) SnippetsByFile(ids []string) map[string][]Snippet {
	result := make(map[string][]Snippet)

	includeAll := len(ids) == 0
	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}

	var primaryPackageSnippet Snippet
	if !includeAll {
		if _, wantsPrimary := idSet[PackageIdentifier]; wantsPrimary {
			primaryPackageSnippet = p.GetSnippet(PackageIdentifier)
		}
	}

	type snippetEntry struct {
		snippet Snippet
		offset  int
		line    int
		column  int
	}

	perFile := make(map[string][]snippetEntry)

	for _, snippet := range p.Snippets() {
		if snippet == nil {
			continue
		}

		if !includeAll {
			include := false
			for _, snippetID := range snippet.IDs() {
				if _, ok := idSet[snippetID]; ok {
					include = true
					break
				}
			}
			if !include && primaryPackageSnippet != nil && snippet == primaryPackageSnippet {
				include = true
			}
			if !include {
				continue
			}
		}

		pos := snippet.Position()
		fileName := pos.Filename
		if fileName == "" {
			switch s := snippet.(type) {
			case *FuncSnippet:
				fileName = s.FileName
			case *ValueSnippet:
				fileName = s.FileName
			case *TypeSnippet:
				fileName = s.FileName
			case *PackageDocSnippet:
				fileName = s.FileName
			}
			if fileName == "" {
				continue
			}
		}

		perFile[fileName] = append(perFile[fileName], snippetEntry{
			snippet: snippet,
			offset:  pos.Offset,
			line:    pos.Line,
			column:  pos.Column,
		})
	}

	for fileName, entries := range perFile {
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].offset != entries[j].offset {
				return entries[i].offset < entries[j].offset
			}
			if entries[i].line != entries[j].line {
				return entries[i].line < entries[j].line
			}
			return entries[i].column < entries[j].column
		})

		ordered := make([]Snippet, len(entries))
		for i, entry := range entries {
			ordered[i] = entry.snippet
		}
		result[fileName] = ordered
	}

	return result
}
