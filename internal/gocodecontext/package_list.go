package gocodecontext

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
)

const (
	// packageListMaxLinesPerModule caps the number of lines we emit for each module in the LLM context string. The returned slice is never truncated.
	packageListMaxLinesPerModule = 60

	packageListGoListTimeout = 45 * time.Second
)

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
// The LLM context string is intentionally opaque (callers should not rely on parsing it; they should directly drop it into an LLM).
func PackageList(absDir, search string, includeDepPackages bool) ([]string, string, error) {
	modRoot, modPath, directDepMods, err := moduleRootAndDirectDeps(absDir)
	if err != nil {
		return nil, "", err
	}

	var re *regexp.Regexp
	if strings.TrimSpace(search) != "" {
		re, err = regexp.Compile(search)
		if err != nil {
			return nil, "", fmt.Errorf("invalid search regexp: %w", err)
		}
	}

	modulePkgs, err := goListImportPaths(modRoot, "./...")
	if err != nil {
		return nil, "", err
	}
	modulePkgs = filterImportPaths(modulePkgs, re)

	byModule := map[string][]string{
		modPath: modulePkgs,
	}

	if includeDepPackages && len(directDepMods) > 0 {
		for _, depMod := range directDepMods {
			depPkgs, err := goListImportPaths(modRoot, depMod+"/...")
			if err != nil {
				return nil, "", err
			}
			depPkgs = filterImportPaths(depPkgs, re)
			depPkgs = filterDepInternalPackages(depMod, depPkgs)
			byModule[depMod] = depPkgs
		}
	}

	// Build the full, sorted package slice (never truncated).
	allSet := make(map[string]struct{}, len(modulePkgs))
	for _, p := range modulePkgs {
		allSet[p] = struct{}{}
	}
	for _, depMod := range directDepMods {
		for _, p := range byModule[depMod] {
			allSet[p] = struct{}{}
		}
	}
	all := make([]string, 0, len(allSet))
	for p := range allSet {
		all = append(all, p)
	}
	sort.Strings(all)

	// Build the LLM context string.
	var sections []string
	sections = append(sections, renderPackageSection(
		fmt.Sprintf("These packages are defined in the current module (%s):", modPath),
		modPath,
		byModule[modPath],
	))

	if includeDepPackages {
		for _, depMod := range directDepMods {
			pkgs := byModule[depMod]
			if len(pkgs) == 0 {
				continue
			}
			sections = append(sections, renderPackageSection(
				fmt.Sprintf("Defined in %s:", depMod),
				depMod,
				pkgs,
			))
		}
	}

	contextStr := strings.TrimSpace(strings.Join(sections, "\n\n"))
	return all, contextStr, nil
}

func moduleRootAndDirectDeps(absDir string) (moduleRoot string, modulePath string, directDepModules []string, err error) {
	dir := filepath.Clean(absDir)
	if !filepath.IsAbs(dir) {
		if resolved, absErr := filepath.Abs(dir); absErr == nil {
			dir = resolved
		}
	}
	info, statErr := os.Stat(dir)
	if statErr != nil {
		return "", "", nil, statErr
	}
	if !info.IsDir() {
		dir = filepath.Dir(dir)
	}

	root, err := findNearestGoModDir(dir)
	if err != nil {
		return "", "", nil, err
	}

	modBytes, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", "", nil, fmt.Errorf("read go.mod: %w", err)
	}
	mf, err := modfile.Parse("go.mod", modBytes, nil)
	if err != nil {
		return "", "", nil, fmt.Errorf("parse go.mod: %w", err)
	}
	if mf.Module == nil || strings.TrimSpace(mf.Module.Mod.Path) == "" {
		return "", "", nil, fmt.Errorf("go.mod is missing a module directive")
	}

	seen := make(map[string]struct{})
	for _, r := range mf.Require {
		if r == nil || r.Indirect {
			continue
		}
		p := strings.TrimSpace(r.Mod.Path)
		if p == "" {
			continue
		}
		seen[p] = struct{}{}
	}

	direct := make([]string, 0, len(seen))
	for p := range seen {
		direct = append(direct, p)
	}
	sort.Strings(direct)

	return root, mf.Module.Mod.Path, direct, nil
}

func findNearestGoModDir(startDir string) (string, error) {
	dir := filepath.Clean(startDir)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found in parent directories starting at %s", startDir)
		}
		dir = parent
	}
}

func goListImportPaths(moduleRoot string, pattern string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), packageListGoListTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "list", "-e", "-f", "{{.ImportPath}}", pattern)
	cmd.Dir = moduleRoot
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("go list %s (dir %s) failed: %w: %s", pattern, moduleRoot, err, strings.TrimSpace(stderr.String()))
	}

	lines := strings.Split(stdout.String(), "\n")
	seen := make(map[string]struct{}, len(lines))
	var out []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		out = append(out, line)
	}
	sort.Strings(out)
	return out, nil
}

func filterImportPaths(paths []string, re *regexp.Regexp) []string {
	if re == nil {
		return paths
	}
	filtered := make([]string, 0, len(paths))
	for _, p := range paths {
		if re.MatchString(p) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

func filterDepInternalPackages(depModule string, importPaths []string) []string {
	filtered := make([]string, 0, len(importPaths))
	for _, p := range importPaths {
		if isInternalToModule(depModule, p) {
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered
}

func isInternalToModule(modulePath, importPath string) bool {
	if modulePath == "" || importPath == "" {
		return false
	}
	if importPath == modulePath {
		return false
	}
	rel, ok := strings.CutPrefix(importPath, modulePath+"/")
	if !ok {
		return false
	}
	if rel == "internal" {
		return true
	}
	if strings.HasPrefix(rel, "internal/") {
		return true
	}
	if strings.Contains(rel, "/internal/") {
		return true
	}
	if strings.HasSuffix(rel, "/internal") {
		return true
	}
	return false
}

func renderPackageSection(title, modulePath string, pkgs []string) string {
	pkgs = append([]string(nil), pkgs...)
	sort.Strings(pkgs)

	rendered := collapsePackageList(modulePath, pkgs, packageListMaxLinesPerModule)

	var b strings.Builder
	b.WriteString(title)
	for _, line := range rendered {
		b.WriteByte('\n')
		b.WriteString("- ")
		b.WriteString(line)
	}
	return strings.TrimSpace(b.String())
}

func collapsePackageList(modulePath string, pkgs []string, maxLines int) []string {
	if maxLines <= 0 || len(pkgs) <= maxLines {
		return pkgs
	}

	tr := newPkgTrie(modulePath, pkgs)
	collapsedPrefixes := tr.chooseCollapsePrefixes(maxLines)

	if len(collapsedPrefixes) == 0 {
		return pkgs
	}

	type collapsedLine struct {
		prefix string
		count  int
	}
	var collapsed []collapsedLine
	for _, pref := range collapsedPrefixes {
		collapsed = append(collapsed, collapsedLine{
			prefix: pref,
			count:  tr.countUnderPrefix(pref),
		})
	}

	shouldOmit := func(p string) bool {
		for _, c := range collapsed {
			if p == c.prefix || strings.HasPrefix(p, c.prefix+"/") {
				return true
			}
		}
		return false
	}

	var out []string
	for _, p := range pkgs {
		if shouldOmit(p) {
			continue
		}
		out = append(out, p)
	}

	for _, c := range collapsed {
		search := "^" + regexp.QuoteMeta(c.prefix) + "(/|$)"
		out = append(out, fmt.Sprintf("%s/... (%d packages omitted; expand with search=%q)", c.prefix, c.count, search))
	}
	sort.Strings(out)
	return out
}

type pkgTrie struct {
	modulePath string
	root       *pkgTrieNode
}

type pkgTrieNode struct {
	segment             string
	fullPrefix          string
	children            map[string]*pkgTrieNode
	isPackage           bool
	subtreePackageCount int
	parent              *pkgTrieNode
}

func newPkgTrie(modulePath string, pkgs []string) *pkgTrie {
	root := &pkgTrieNode{
		fullPrefix: modulePath,
		children:   make(map[string]*pkgTrieNode),
	}

	tr := &pkgTrie{
		modulePath: modulePath,
		root:       root,
	}

	for _, p := range pkgs {
		tr.insert(p)
	}
	tr.root.computeSubtreeCounts()
	return tr
}

func (tr *pkgTrie) insert(importPath string) {
	n := tr.root
	if importPath == tr.modulePath {
		n.isPackage = true
		return
	}
	rel, ok := strings.CutPrefix(importPath, tr.modulePath+"/")
	if !ok {
		// Not expected, but keep it renderable by placing it under the root as a single segment.
		rel = importPath
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" {
			continue
		}
		if n.children == nil {
			n.children = make(map[string]*pkgTrieNode)
		}
		child, ok := n.children[seg]
		if !ok {
			prefix := seg
			if n.fullPrefix != "" {
				prefix = n.fullPrefix + "/" + seg
			}
			child = &pkgTrieNode{
				segment:    seg,
				fullPrefix: prefix,
				children:   make(map[string]*pkgTrieNode),
				parent:     n,
			}
			n.children[seg] = child
		}
		n = child
	}
	n.isPackage = true
}

func (n *pkgTrieNode) computeSubtreeCounts() int {
	count := 0
	if n.isPackage {
		count++
	}
	for _, ch := range n.children {
		count += ch.computeSubtreeCounts()
	}
	n.subtreePackageCount = count
	return count
}

func (tr *pkgTrie) nodes() []*pkgTrieNode {
	var out []*pkgTrieNode
	var walk func(n *pkgTrieNode)
	walk = func(n *pkgTrieNode) {
		out = append(out, n)
		for _, ch := range n.children {
			walk(ch)
		}
	}
	walk(tr.root)
	return out
}

func (tr *pkgTrie) countUnderPrefix(prefix string) int {
	if prefix == tr.modulePath {
		return tr.root.subtreePackageCount
	}
	rel, ok := strings.CutPrefix(prefix, tr.modulePath+"/")
	if !ok {
		return 0
	}
	n := tr.root
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" {
			continue
		}
		next := n.children[seg]
		if next == nil {
			return 0
		}
		n = next
	}
	return n.subtreePackageCount
}

func (tr *pkgTrie) chooseCollapsePrefixes(maxLines int) []string {
	currentLines := tr.root.subtreePackageCount
	if currentLines <= maxLines {
		return nil
	}

	candidates := make([]*pkgTrieNode, 0)
	for _, n := range tr.nodes() {
		if n == tr.root {
			continue
		}
		if n.subtreePackageCount <= 1 {
			continue
		}
		candidates = append(candidates, n)
	}

	// Prefer nodes that (1) reduce many lines and (2) represent a large fraction of
	// their parent subtree. This tends to collapse "heavy" subtrees like
	// internal/gen without hiding sibling packages under internal/.
	depth := func(n *pkgTrieNode) int {
		d := 0
		for p := n.parent; p != nil && p != tr.root; p = p.parent {
			d++
		}
		return d
	}
	sort.Slice(candidates, func(i, j int) bool {
		pi := candidates[i].parent
		pj := candidates[j].parent
		parentCountI := 1
		parentCountJ := 1
		if pi != nil && pi.subtreePackageCount > 0 {
			parentCountI = pi.subtreePackageCount
		}
		if pj != nil && pj.subtreePackageCount > 0 {
			parentCountJ = pj.subtreePackageCount
		}
		si := candidates[i].subtreePackageCount - 1
		sj := candidates[j].subtreePackageCount - 1

		scoreI := float64(si) * float64(candidates[i].subtreePackageCount) / float64(parentCountI)
		scoreJ := float64(sj) * float64(candidates[j].subtreePackageCount) / float64(parentCountJ)
		if scoreI != scoreJ {
			return scoreI > scoreJ
		}
		if si != sj {
			return si > sj
		}
		di := depth(candidates[i])
		dj := depth(candidates[j])
		if di != dj {
			return di > dj
		}
		return candidates[i].fullPrefix < candidates[j].fullPrefix
	})

	var chosen []string
	var chosenNodes []*pkgTrieNode
	isDescendantOfChosen := func(n *pkgTrieNode) bool {
		for _, c := range chosenNodes {
			for p := n; p != nil; p = p.parent {
				if p == c {
					return true
				}
			}
		}
		return false
	}

	for _, cand := range candidates {
		if currentLines <= maxLines {
			break
		}
		if isDescendantOfChosen(cand) {
			continue
		}
		// Skip if this would overlap a chosen descendant (we already skipped that via isDescendantOfChosen).
		chosenNodes = append(chosenNodes, cand)
		chosen = append(chosen, cand.fullPrefix)
		currentLines -= cand.subtreePackageCount - 1
	}

	sort.Strings(chosen)
	return chosen
}
