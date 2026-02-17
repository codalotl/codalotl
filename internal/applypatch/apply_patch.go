package applypatch

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ApplyPatchGrammar defines the Lark-style grammar for the "*** Begin Patch" format parsed by this package.
const ApplyPatchGrammar = `start: begin_patch hunk+ end_patch
begin_patch: "*** Begin Patch" LF
end_patch: "*** End Patch" LF?

hunk: add_hunk | delete_hunk | update_hunk
add_hunk: "*** Add File: " filename LF add_line+
delete_hunk: "*** Delete File: " filename LF
update_hunk: "*** Update File: " filename LF change_move? change?

filename: /(.+)/
add_line: "+" /(.+)/ LF -> line

change_move: "*** Move to: " filename LF
change: (change_context | change_line)+ eof_line?
change_context: ("@@" | "@@ " /(.+)/) LF
change_line: ("+" | "-" | " ") /(.+)/ LF
eof_line: "*** End of File" LF
%import common.LF`

var errInvalidPatch = errors.New("invalid patch")

// IsInvalidPatch reports whether err (as returned from ApplyPatch) indicates that the patch itself was invalid: malformed input, unsafe/unsupported paths, or a
// hunk that could not be matched/applied.
//
// It returns false for non-patch problems such as permission or other filesystem I/O failures while applying.
func IsInvalidPatch(err error) bool {
	return errors.Is(err, errInvalidPatch)
}
func invalidPatchError(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(errInvalidPatch, err)
}

// ApplyPatch parses and applies a patch in the format defined by the grammar in ApplyPatchGrammar. It applies changes rooted at cwdAbsPath, which must be an absolute
// path, and returns the relative file-level changes that were applied.
//
// ApplyPatch applies the "*** Begin Patch" format.
//
// # Semantics
//
// Files
//   - *** Add File: <path> → write file with the following + lines as content (overwriting if it already exists)
//   - *** Delete File: <path> → delete the file
//   - *** Update File: <path> → (optional) "*** Move to: <newpath>" rename, then one or more change hunks
//
// Change hunks (inside *** Update File)
//   - "@@" starts a new hunk (a new, noncontiguous change region). Text after "@@" (optionally starting with a single space) is treated as an anchor that narrows
//     where the hunk applies. Anchors are matched before context; multiple consecutive "@@" lines zoom further in.
//   - Hunks must appear in the same order as the target file.
//   - Line prefixes within a hunk:
//   - ' ' context line — text after leading whitespace must match; indentation may remap between tabs and spaces to mirror the file being patched.
//   - '-' delete this line at the current anchored spot (same indentation remapping rules).
//   - '+' insert this line; indentation uses the remapped whitespace when available, otherwise it is taken exactly as written in the patch.
//   - "*** End of File" is accepted as a marker and ignored.
//   - No line numbers; matching is literal. The first column is the control prefix; everything after it is raw text (including leading/trailing spaces). Newlines
//     are LF.
//
// Hunk construction guidelines
//   - Use one "@@" per noncontiguous edit region.
//   - Include at least 1 pre-context and 1 post-context line when not at a boundary; choose distinctive lines. Increase context (often to 2–3 on the ambiguous
//     side) until the sequence is unique in the file.
//   - At start of file you may omit pre-context; at end of file omit post-context or use "*** End of File".
//   - Replacements can be N '-' lines followed by M '+' lines; interleaving is allowed but usually unnecessary.
//
// FAQs
//   - Do '+' and '-' have to interleave? No. Group deletes then inserts to replace a block; insert-only and delete-only hunks are valid.
//   - Do I need post-context? Not required, but recommended. If the post-context is weak (e.g., just "}"), add a more distinctive neighbor so the hunk is unique.
//   - Do I need "@@" at all? Optional in the grammar, but best practice is one per noncontiguous region for clarity and safer application.
//   - How many context lines should I include? Start with 1 before + 1 after; if the sequence isn’t unique, grow to 2–3 on the ambiguous side.
//   - What does the text after "@@" do? It narrows the search scope before matching context. The applier first searches for anchor text (discarding an optional leading
//     space), then falls back to trimming trailing whitespace, trimming both leading and trailing whitespace, and finally converting select unicode punctuation (e.g.,
//     em dash) to ASCII before giving up.
//   - How do I append at EOF? Use pre-context for the last real line and add '+' lines.
//   - How do I encode lines that themselves start with '+', '-', or space? The first column is control; the next character is content. Example: "++foo" adds a line
//     that begins with '+'.
//   - Can hunks touch or overlap? Adjacent is fine; overlapping is invalid—merge into one hunk instead.
//   - What if the context matches in multiple places? ApplyPatch uses the first matching span; choose context that makes the anchor unique.
func ApplyPatch(cwdAbsPath string, patch string) ([]FileChange, error) {
	if !filepath.IsAbs(cwdAbsPath) {
		return nil, fmt.Errorf("cwdAbsPath must be absolute: %q", cwdAbsPath)
	}
	root := filepath.Clean(cwdAbsPath)

	parsed, err := parsePatch(patch)
	if err != nil {
		return nil, invalidPatchError(err)
	}
	var changes []FileChange
	for idx := range parsed.Hunks {
		h := &parsed.Hunks[idx]
		origPath := h.Path
		relPath, err := resolvePatchPath(root, origPath)
		if err != nil {
			return nil, invalidPatchError(fmt.Errorf("hunk %d path %q: %w", idx+1, origPath, err))
		}
		h.Path = relPath

		if h.MoveTo != "" {
			origMove := h.MoveTo
			relMove, err := resolvePatchPath(root, origMove)
			if err != nil {
				return nil, invalidPatchError(fmt.Errorf("hunk %d move %q: %w", idx+1, origMove, err))
			}
			h.MoveTo = relMove
		}
	}

	for idx, h := range parsed.Hunks {
		switch h.Kind {
		case hunkAdd:
			if err := applyAdd(root, h); err != nil {
				return nil, fmt.Errorf("add hunk %d (%s): %w", idx+1, h.Path, err)
			}
			changes = append(changes, FileChange{Path: h.Path, Kind: FileChangeAdded})
		case hunkDelete:
			if err := applyDelete(root, h); err != nil {
				return nil, fmt.Errorf("delete hunk %d (%s): %w", idx+1, h.Path, err)
			}
			changes = append(changes, FileChange{Path: h.Path, Kind: FileChangeDeleted})
		case hunkUpdate:
			if err := applyUpdate(root, h); err != nil {
				return nil, fmt.Errorf("update hunk %d (%s): %w", idx+1, h.Path, err)
			}
			if h.MoveTo != "" {
				changes = append(changes,
					FileChange{Path: h.Path, Kind: FileChangeDeleted},
					FileChange{Path: h.MoveTo, Kind: FileChangeAdded},
				)
			} else {
				changes = append(changes, FileChange{Path: h.Path, Kind: FileChangeModified})
			}
		default:
			return nil, fmt.Errorf("unknown hunk kind for %s", h.Path)
		}
	}
	return changes, nil
}

// ---------- Patch data structures ----------

type hunkKind int

const (
	_ hunkKind = iota
	hunkAdd
	hunkDelete
	hunkUpdate
)

type patchFile struct {
	Hunks []fileHunk
}

type fileHunk struct {
	Kind        hunkKind
	Path        string
	MoveTo      string
	AddLines    []string
	ChangeSets  []changeSet
	NoFinalNL   bool
	rawLineSpan [2]int
}

type changeSet struct {
	Anchors []string
	Lines   []changeLine
}

type changeLine struct {
	Op   byte
	Text string
}

type FileChangeKind int

const (
	_ FileChangeKind = iota
	FileChangeAdded
	FileChangeModified
	FileChangeDeleted
)

type FileChange struct {
	Path string
	Kind FileChangeKind
}

var anchorASCIIReplacements = map[rune]string{
	'—': "-",
	'–': "-",
	'―': "-",
	'“': `"`,
	'”': `"`,
	'‘': "'",
	'’': "'",
	'•': "*",
	'·': "*",
	'…': "...",
	'×': "x",
}

// ---------- Filesystem helpers ----------

func ensureParentDir(path string) error {
	if d := filepath.Dir(path); d != "." && d != "" {
		return os.MkdirAll(d, 0o777)
	}
	return nil
}

func resolvePatchPath(root, raw string) (string, error) {
	path := filepath.FromSlash(raw)
	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(root, path))
	}

	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return "", fmt.Errorf("path %q resolves to working directory root", raw)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes working directory %s", raw, root)
	}

	return filepath.ToSlash(rel), nil
}

// ---------- Parsing ----------

type parser struct {
	lines []string
	idx   int
}

func newParser(input string) *parser {
	normalized := strings.ReplaceAll(input, "\r\n", "\n")
	return &parser{lines: strings.Split(normalized, "\n")}
}

func (p *parser) eof() bool { return p.idx >= len(p.lines) }

func (p *parser) peek() (string, bool) {
	if p.eof() {
		return "", false
	}
	return p.lines[p.idx], true
}

func (p *parser) next() (string, bool) {
	line, ok := p.peek()
	if !ok {
		return "", false
	}
	p.idx++
	return line, true
}

func (p *parser) lineNumber() int { return p.idx + 1 }

func (p *parser) mark() int { return p.idx }

func parsePatch(input string) (*patchFile, error) {
	p := newParser(input)
	first, ok := p.next()
	if !ok || strings.TrimSpace(first) != "*** Begin Patch" {
		return nil, errors.New(`patch must start with "*** Begin Patch"`)
	}

	var pf patchFile
	for {
		line, ok := p.peek()
		if !ok {
			return nil, errors.New(`unexpected end of input; expected hunk or "*** End Patch"`)
		}
		if strings.TrimSpace(line) == "*** End Patch" {
			p.next()
			break
		}

		h, err := parseFileHunk(p)
		if err != nil {
			return nil, err
		}
		pf.Hunks = append(pf.Hunks, h)
	}

	for !p.eof() {
		if strings.TrimSpace(p.lines[p.idx]) != "" {
			return nil, fmt.Errorf("unexpected trailing content at line %d", p.lineNumber())
		}
		p.idx++
	}
	return &pf, nil
}

func parseFileHunk(p *parser) (fileHunk, error) {
	start := p.mark()
	rawHeader, ok := p.next()
	if !ok {
		return fileHunk{}, errors.New("unexpected end of input while reading hunk header")
	}
	header := strings.TrimSpace(rawHeader)

	switch {
	case strings.HasPrefix(header, "*** Add File: "):
		path := strings.TrimPrefix(header, "*** Add File: ")
		if path == "" {
			return fileHunk{}, fmt.Errorf("empty path for Add at line %d", start+1)
		}
		addLines, err := parseAddLines(p, path)
		if err != nil {
			return fileHunk{}, err
		}
		return fileHunk{
			Kind:        hunkAdd,
			Path:        path,
			AddLines:    addLines,
			rawLineSpan: [2]int{start, p.mark()},
		}, nil

	case strings.HasPrefix(header, "*** Delete File: "):
		path := strings.TrimPrefix(header, "*** Delete File: ")
		if path == "" {
			return fileHunk{}, fmt.Errorf("empty path for Delete at line %d", start+1)
		}
		return fileHunk{
			Kind:        hunkDelete,
			Path:        path,
			rawLineSpan: [2]int{start, p.mark()},
		}, nil

	case strings.HasPrefix(header, "*** Update File: "):
		path := strings.TrimPrefix(header, "*** Update File: ")
		if path == "" {
			return fileHunk{}, fmt.Errorf("empty path for Update at line %d", start+1)
		}
		h := fileHunk{Kind: hunkUpdate, Path: path}
		if next, ok := p.peek(); ok && strings.HasPrefix(strings.TrimSpace(next), "*** Move to: ") {
			moveToRaw, _ := p.next()
			moveTo := strings.TrimSpace(moveToRaw)
			moveTo = strings.TrimPrefix(moveTo, "*** Move to: ")
			if moveTo == "" {
				return fileHunk{}, fmt.Errorf("empty destination in Move to at line %d", p.mark())
			}
			h.MoveTo = moveTo
		}
		sets, err := parseChangeSets(p, path)
		if err != nil {
			return fileHunk{}, err
		}
		h.ChangeSets = sets
		h.rawLineSpan = [2]int{start, p.mark()}
		return h, nil
	}

	return fileHunk{}, fmt.Errorf("expected hunk header at line %d; got %q", start+1, rawHeader)
}

func parseAddLines(p *parser, path string) ([]string, error) {
	var lines []string
	for {
		next, ok := p.peek()
		if !ok {
			return nil, fmt.Errorf("unterminated Add for %s", path)
		}
		if isFileBoundary(next) {
			break
		}
		if !strings.HasPrefix(next, "+") {
			return nil, fmt.Errorf("add for %s: expected '+' line at %d, got: %q", path, p.lineNumber(), next)
		}
		lines = append(lines, strings.TrimPrefix(next, "+"))
		p.next()
	}
	return lines, nil
}

func parseChangeSets(p *parser, path string) ([]changeSet, error) {
	var sets []changeSet
	var cur changeSet
	started := false
	flush := func() error {
		if !started {
			return nil
		}
		if len(cur.Lines) == 0 {
			return fmt.Errorf("update for %s: anchor provided without any changes", path)
		}
		sets = append(sets, cur)
		cur = changeSet{}
		started = false
		return nil
	}

	for {
		next, ok := p.peek()
		if !ok {
			return nil, fmt.Errorf("unterminated Update for %s", path)
		}
		if isFileBoundary(next) {
			break
		}
		if strings.TrimSpace(next) == "*** End of File" {
			p.next()
			continue
		}
		if strings.HasPrefix(next, "@@") {
			header, _ := p.next()
			anchor := parseAnchorHeader(header)
			if started && len(cur.Lines) > 0 {
				if err := flush(); err != nil {
					return nil, err
				}
			}
			if !started {
				cur = changeSet{}
				started = true
			}
			if anchor != "" {
				cur.Anchors = append(cur.Anchors, anchor)
			}
			continue
		}
		if len(next) > 0 && (next[0] == '+' || next[0] == '-' || next[0] == ' ') {
			if !started {
				cur = changeSet{}
				started = true
			}
			cur.Lines = append(cur.Lines, changeLine{Op: next[0], Text: next[1:]})
			p.next()
			continue
		}
		return nil, fmt.Errorf("malformed update for %s at line %d: %q", path, p.lineNumber(), next)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return sets, nil
}

func parseAnchorHeader(line string) string {
	rest := strings.TrimPrefix(line, "@@")
	if len(rest) > 0 && rest[0] == ' ' {
		rest = rest[1:]
	}
	return rest
}

func isFileBoundary(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "*** End Patch" ||
		strings.HasPrefix(trimmed, "*** Add File: ") ||
		strings.HasPrefix(trimmed, "*** Delete File: ") ||
		strings.HasPrefix(trimmed, "*** Update File: ")
}

// ---------- Apply: Add/Delete/Update ----------

func applyAdd(root string, h fileHunk) error {
	osPath := filepath.Join(root, filepath.FromSlash(h.Path))
	if err := ensureParentDir(osPath); err != nil {
		return err
	}
	content := strings.Join(h.AddLines, "\n")
	if len(h.AddLines) > 0 {
		content += "\n"
	}
	return os.WriteFile(osPath, []byte(content), 0o644)
}

func applyDelete(root string, h fileHunk) error {
	osPath := filepath.Join(root, filepath.FromSlash(h.Path))
	if err := os.Remove(osPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func applyUpdate(root string, h fileHunk) error {
	src := filepath.Join(root, filepath.FromSlash(h.Path))
	dst := src
	if h.MoveTo != "" {
		dst = filepath.Join(root, filepath.FromSlash(h.MoveTo))
	}

	snapshot, err := readTextFile(src)
	if err != nil {
		readErr := fmt.Errorf("read %s: %w", h.Path, err)
		if errors.Is(err, fs.ErrNotExist) {
			return invalidPatchError(readErr)
		}
		return readErr
	}

	updatedLines, err := applyChangeSetsToLines(snapshot.lines, h.ChangeSets)
	if err != nil {
		return invalidPatchError(err)
	}

	finalNewline := len(updatedLines) > 0
	content := joinLines(updatedLines, snapshot.newline, finalNewline)

	if err := ensureParentDir(dst); err != nil {
		return err
	}
	if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
		return err
	}
	if dst != src {
		_ = os.Remove(src)
	}
	return nil
}

// ---------- Text helpers ----------

type textFile struct {
	lines           []string
	newline         string
	hadFinalNewline bool
}

func readTextFile(path string) (textFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return textFile{}, err
	}
	content := string(data)
	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
		content = strings.ReplaceAll(content, "\r\n", "\n")
	}
	hadFinal := strings.HasSuffix(content, "\n")
	parts := strings.Split(content, "\n")
	if hadFinal {
		parts = parts[:len(parts)-1]
	}
	return textFile{lines: parts, newline: newline, hadFinalNewline: hadFinal}, nil
}

func joinLines(lines []string, newline string, final bool) string {
	switch len(lines) {
	case 0:
		if final {
			return newline
		}
		return ""
	}
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteString(newline)
		}
		b.WriteString(line)
	}
	if final {
		b.WriteString(newline)
	}
	return b.String()
}

func applyChangeSetsToLines(orig []string, sets []changeSet) ([]string, error) {
	out := make([]string, 0, len(orig))
	pos := 0
	indentMap := make(map[string]string)
	for csi, cs := range sets {
		searchFrom := pos
		if len(cs.Anchors) > 0 {
			anchorIdx, err := locateAnchors(orig, cs.Anchors, pos)
			if err != nil {
				return nil, fmt.Errorf("hunk %d: %w", csi+1, err)
			}
			searchFrom = anchorIdx
		}

		pattern := expectedContext(cs)
		start := pos
		if len(pattern) > 0 {
			idx, err := findContextWithIndent(orig, pattern, searchFrom, indentMap)
			if err != nil {
				return nil, fmt.Errorf("hunk %d: %w", csi+1, err)
			}
			start = idx
		} else if len(cs.Anchors) > 0 {
			start = searchFrom
		} else {
			// No anchors and no context/deletions means this change set is pure insertion. Codex golden cases treat that as appending at EOF.
			start = len(orig)
		}
		out = append(out, orig[pos:start]...)

		i := start
		for li, cl := range cs.Lines {
			switch cl.Op {
			case ' ':
				if i >= len(orig) || !linesMatchAndRecord(cl.Text, orig[i], indentMap) {
					got := ""
					if i < len(orig) {
						got = orig[i]
					}
					return nil, fmt.Errorf("hunk %d, line %d: context mismatch: want %q, got %q", csi+1, li+1, cl.Text, got)
				}
				out = append(out, orig[i])
				i++
			case '-':
				if i >= len(orig) || !linesMatchAndRecord(cl.Text, orig[i], indentMap) {
					got := ""
					if i < len(orig) {
						got = orig[i]
					}
					return nil, fmt.Errorf("hunk %d, line %d: delete mismatch: want %q, got %q", csi+1, li+1, cl.Text, got)
				}
				i++
			case '+':
				out = append(out, applyIndentMapping(cl.Text, indentMap))
			default:
				return nil, fmt.Errorf("hunk %d: invalid change op %q", csi+1, string(cl.Op))
			}
		}
		pos = i
	}
	out = append(out, orig[pos:]...)
	return out, nil
}

func expectedContext(cs changeSet) []string {
	var pattern []string
	for _, cl := range cs.Lines {
		if cl.Op == ' ' || cl.Op == '-' {
			pattern = append(pattern, cl.Text)
		}
	}
	return pattern
}

func locateAnchors(lines []string, anchors []string, from int) (int, error) {
	pos := from
	for ai, raw := range anchors {
		if raw == "" {
			continue
		}
		idx, ok := findAnchor(lines, raw, pos)
		if !ok {
			return 0, fmt.Errorf("anchor %d (%q) not found starting at line %d", ai+1, raw, pos+1)
		}
		pos = idx
	}
	return pos, nil
}

func findAnchor(lines []string, anchor string, from int) (int, bool) {
	for _, candidate := range anchorVariants(anchor) {
		if candidate == "" {
			return from, true
		}
		for i := from; i < len(lines); i++ {
			if strings.Contains(lines[i], candidate) {
				return i, true
			}
		}
	}
	return 0, false
}

func anchorVariants(anchor string) []string {
	var variants []string
	addVariant := func(s string) {
		if len(variants) == 0 || variants[len(variants)-1] != s {
			variants = append(variants, s)
		}
	}

	addVariant(anchor)
	trimTrailing := strings.TrimRight(anchor, " \t")
	addVariant(trimTrailing)
	trimBoth := strings.TrimLeft(trimTrailing, " \t")
	addVariant(trimBoth)
	ascii := convertAnchorToASCII(trimBoth)
	addVariant(ascii)
	return variants
}

func convertAnchorToASCII(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	changed := false
	for _, r := range s {
		if repl, ok := anchorASCIIReplacements[r]; ok {
			b.WriteString(repl)
			changed = true
			continue
		}
		b.WriteRune(r)
	}
	if !changed {
		return s
	}
	return b.String()
}

func findContextWithIndent(orig, pattern []string, from int, indentMap map[string]string) (int, error) {
	if len(pattern) == 0 {
		return from, nil
	}
	snippet := ""
	if len(pattern) > 0 {
		snippet = pattern[0]
	}
	// Prefer exact matches without relying on indentation remapping when available.
	for i := from; i+len(pattern) <= len(orig); i++ {
		match := true
		for j := 0; j < len(pattern); j++ {
			if pattern[j] != orig[i+j] {
				match = false
				break
			}
		}
		if match {
			return i, nil
		}
	}
	for i := from; i+len(pattern) <= len(orig); i++ {
		trial := copyIndentMap(indentMap)
		ok := true
		for j := 0; j < len(pattern); j++ {
			if !linesMatchAndRecord(pattern[j], orig[i+j], trial) {
				ok = false
				break
			}
		}
		if ok {
			for k, v := range trial {
				indentMap[k] = v
			}
			return i, nil
		}
	}
	return -1, fmt.Errorf("context not found near line %d (first context: %q)", from+1, snippet)
}

func linesMatchAndRecord(patch, actual string, indentMap map[string]string) bool {
	if patch == actual {
		return true
	}
	pIndent, pRest := splitIndent(patch)
	aIndent, aRest := splitIndent(actual)
	if pRest != aRest {
		return false
	}
	if existing, ok := indentMap[pIndent]; ok {
		return existing == aIndent
	}
	if pIndent != "" {
		indentMap[pIndent] = aIndent
	}
	return true
}

func applyIndentMapping(line string, indentMap map[string]string) string {
	indent, rest := splitIndent(line)
	if indent == "" {
		return line
	}
	if mapped, ok := indentMap[indent]; ok {
		if mapped == indent {
			return line
		}
		return mapped + rest
	}
	return line
}

func splitIndent(s string) (string, string) {
	i := 0
	for i < len(s) {
		if s[i] != ' ' && s[i] != '\t' {
			break
		}
		i++
	}
	return s[:i], s[i:]
}

func copyIndentMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return make(map[string]string)
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
