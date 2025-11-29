package renamebot

import (
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"sort"
	"strings"
)

// Used to produce a summary of naming on a per-type basis. Example output based on this info:
// go/types.Token:
//   function vars:
//     tok: 3 (*go/types.Token)
//     toks: 1 ([]go/types.Token)
//   parameters:
//     toks: 1 ([]go/types.Token)
//   receiver:
//     (none)

type packageSummary struct {
	// key is root type (ex: "MyType", not "*MyType")
	// keys can include package selector if type isn't package local.
	summaryPerType map[string]*typeSummary

	// maps filename (base) to set of root types that the filename uses.
	fileToRootTypeSet map[string]map[string]struct{}
}

// relevantForFile returns a new packageSummary but with only root types that file has identifiers for.
func (ps *packageSummary) relevantForFile(fileName string) *packageSummary {
	if ps == nil || fileName == "" {
		return &packageSummary{summaryPerType: map[string]*typeSummary{}, fileToRootTypeSet: map[string]map[string]struct{}{}}
	}

	// Gather root types used by this file
	rts, ok := ps.fileToRootTypeSet[fileName]
	if !ok || len(rts) == 0 {
		return &packageSummary{summaryPerType: map[string]*typeSummary{}, fileToRootTypeSet: map[string]map[string]struct{}{fileName: {}}}
	}

	out := &packageSummary{
		summaryPerType:    make(map[string]*typeSummary, len(rts)),
		fileToRootTypeSet: map[string]map[string]struct{}{fileName: {}},
	}

	// Copy set for the file into the outgoing summary
	for rt := range rts {
		if out.fileToRootTypeSet[fileName] == nil {
			out.fileToRootTypeSet[fileName] = make(map[string]struct{}, len(rts))
		}
		out.fileToRootTypeSet[fileName][rt] = struct{}{}

		ts := ps.summaryPerType[rt]
		if ts == nil {
			continue
		}
		// Deep copy the typeSummary so that mutations on the filtered view do not affect the original
		copyMap := func(m map[typeSummaryKey]int) map[typeSummaryKey]int {
			if len(m) == 0 {
				return map[typeSummaryKey]int{}
			}
			nm := make(map[typeSummaryKey]int, len(m))
			for k, v := range m {
				nm[k] = v
			}
			return nm
		}
		out.summaryPerType[rt] = &typeSummary{
			rootType:  ts.rootType,
			all:       copyMap(ts.all),
			funcVars:  copyMap(ts.funcVars),
			params:    copyMap(ts.params),
			receivers: copyMap(ts.receivers),
		}
	}

	return out
}

// rejectUnified mutates ps.summaryPerType. It deletes an entire type summary if it's already unified, meaning:
//   - all receivers are named the same thing (if any), AND
//   - all function vars are named the same thing, which is named the same thing as the function params.
//   - (NOTE: receivers can be differently named than function vars/params, since receivers are often just 1 or 2 letters)
//
// One basic case where a type be rejected is because there's only a single use of that type.
//
// Note that if the complete type varies (ex: function vars named "foos" for []Foo and "foo" for Foo), it's not considered unified per this definition, and we wouldn't reject it.
func (ps *packageSummary) rejectUnified() {
	if ps == nil || len(ps.summaryPerType) == 0 {
		return
	}

	// Helper: returns (identifier, completeType, ok). ok means the map is empty (vacuously unified) or
	// has a single identifier and single completeType across all entries.
	singleNameAndType := func(m map[typeSummaryKey]int) (string, string, bool) {
		if len(m) == 0 {
			return "", "", true
		}
		var (
			name  string
			ctype string
			set   bool
		)
		for k := range m {
			if !set {
				name = k.identifier
				ctype = k.completeType
				set = true
				continue
			}
			if k.identifier != name || k.completeType != ctype {
				return "", "", false
			}
		}
		return name, ctype, true
	}

	// Helper: returns (identifier, ok). ok means the map is empty (vacuously unified)
	// or has a single identifier across all entries. Ignores type differences.
	singleName := func(m map[typeSummaryKey]int) (string, bool) {
		if len(m) == 0 {
			return "", true
		}
		var (
			name string
			set  bool
		)
		for k := range m {
			if !set {
				name = k.identifier
				set = true
				continue
			}
			if k.identifier != name {
				return "", false
			}
		}
		return name, true
	}

	for rt, ts := range ps.summaryPerType {
		if ts == nil {
			delete(ps.summaryPerType, rt)
			continue
		}

		// If there is only a single use across all identifiers, reject.
		total := 0
		for _, c := range ts.all {
			total += c
		}
		if total <= 1 {
			delete(ps.summaryPerType, rt)
			continue
		}

		// Receivers must be uniformly named (allowing none). Ignore receiver type differences (pointer vs value).
		_, recvOK := singleName(ts.receivers)
		if !recvOK {
			continue
		}

		// Vars and params must be uniformly named within their groups. If both groups are non-empty,
		// they must also match each other in both name and complete type. If either group is empty,
		// treat it as vacuously unified.
		vName, vType, varsOK := singleNameAndType(ts.funcVars)
		pName, pType, paramsOK := singleNameAndType(ts.params)

		// Determine whether the vars/params situation is unified enough to reject:
		// - If both present: both groups must be uniform and match in name+type
		// - If only vars present: vars must be uniform
		// - If only params present: params must be uniform
		// - If both empty: do NOT treat as unified (avoid deleting types with only receivers or type-only uses)
		if !varsOK || !paramsOK {
			continue
		}
		var groupsUnified bool
		switch {
		case len(ts.funcVars) > 0 && len(ts.params) > 0:
			groupsUnified = vName == pName && vType == pType
		case len(ts.funcVars) > 0 && len(ts.params) == 0:
			groupsUnified = true
		case len(ts.funcVars) == 0 && len(ts.params) > 0:
			groupsUnified = true
		default: // both empty
			groupsUnified = false
		}

		if recvOK && groupsUnified {
			delete(ps.summaryPerType, rt)
		}
	}
}

//

func (ps *packageSummary) String() string {
	if ps == nil || len(ps.summaryPerType) == 0 {
		return ""
	}

	// Sort root types for deterministic output
	var typesOrdered []string
	for rt := range ps.summaryPerType {
		typesOrdered = append(typesOrdered, rt)
	}
	sort.Strings(typesOrdered)

	var b strings.Builder
	for i, rt := range typesOrdered {
		ts := ps.summaryPerType[rt]
		if ts == nil {
			continue
		}

		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("%s:\n", ts.rootType))

		writeSection := func(header string, entries []typeSummaryEntry) {
			if len(entries) == 0 {
				return
			}
			b.WriteString(fmt.Sprintf("  %s:\n", header))
			for _, e := range entries {
				b.WriteString(fmt.Sprintf("    %s: %d (%s)\n", e.identifier, e.count, e.completeType))
			}
		}

		writeSection("func vars", ts.getFuncVars())
		writeSection("params", ts.getParams())
		writeSection("receiver", ts.getReceivers())
	}

	return b.String()
}

// tests=true means only tests
func newPackageSummary(pkg *gocode.Package, tests bool) (*packageSummary, error) {
	ids, _, err := typedIdentifiersInPackage(pkg, tests)
	if err != nil {
		return nil, err
	}

	ps := &packageSummary{summaryPerType: map[string]*typeSummary{}, fileToRootTypeSet: map[string]map[string]struct{}{}}

	for _, id := range ids {
		if id == nil {
			continue
		}
		// We only summarize by named root types. Builtins like int/string are ignored.
		if !id.IsNamedType || id.RootType == "" {
			continue
		}

		// Track file -> root type set
		if ps.fileToRootTypeSet[id.FileName] == nil {
			ps.fileToRootTypeSet[id.FileName] = map[string]struct{}{}
		}
		ps.fileToRootTypeSet[id.FileName][id.RootType] = struct{}{}

		ts := ps.summaryPerType[id.RootType]
		if ts == nil {
			ts = &typeSummary{
				rootType:  id.RootType,
				all:       map[typeSummaryKey]int{},
				funcVars:  map[typeSummaryKey]int{},
				params:    map[typeSummaryKey]int{},
				receivers: map[typeSummaryKey]int{},
			}
			ps.summaryPerType[id.RootType] = ts
		}

		key := typeSummaryKey{
			kind:         id.Kind,
			identifier:   id.Identifier,
			completeType: id.CompleteType,
		}

		ts.all[key]++

		switch id.Kind {
		case IdentifierKindFuncVar, IdentifierKindFuncConst:
			// Only variables belong here; consts are function-scoped too but keep them out of vars category.
			if id.Kind == IdentifierKindFuncVar {
				ts.funcVars[key]++
			}
		case IdentifierKindFuncParam:
			ts.params[key]++
		case IdentifierKindFuncReceiver:
			ts.receivers[key]++
		}
	}

	return ps, nil
}

type typeSummary struct {
	rootType string

	all map[typeSummaryKey]int

	funcVars  map[typeSummaryKey]int // kind must be IdentifierKindFuncVar
	params    map[typeSummaryKey]int // kind must be IdentifierKindFuncParam
	receivers map[typeSummaryKey]int // kind must be IdentifierKindFuncReceiver
}

type typeSummaryKey struct {
	kind         IdentifierKind
	identifier   string
	completeType string
}

type typeSummaryEntry struct {
	typeSummaryKey
	count int
}

// returns sorted by count desc
func (ts *typeSummary) getFuncVars() []typeSummaryEntry {
	return sortEntries(ts.funcVars)
}

func (ts *typeSummary) getParams() []typeSummaryEntry {
	return sortEntries(ts.params)
}

func (ts *typeSummary) getReceivers() []typeSummaryEntry {
	return sortEntries(ts.receivers)
}

func sortEntries(m map[typeSummaryKey]int) []typeSummaryEntry {
	if len(m) == 0 {
		return nil
	}
	out := make([]typeSummaryEntry, 0, len(m))
	for k, v := range m {
		out = append(out, typeSummaryEntry{typeSummaryKey: k, count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		// deterministic tiebreakers
		if out[i].identifier != out[j].identifier {
			return out[i].identifier < out[j].identifier
		}
		return out[i].completeType < out[j].completeType
	})
	return out
}
