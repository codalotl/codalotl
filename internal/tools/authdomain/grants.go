package authdomain

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var ErrAuthorizerCannotAcceptGrants = errors.New("authdomain: authorizer cannot accept grants")

type grantMessageAcceptor interface {
	addGrantUserMessage(userMessage string)
}

func AddGrantsFromUserMessage(authorizer Authorizer, userMessage string) error {
	if authorizer == nil {
		return ErrAuthorizerCannotAcceptGrants
	}

	acceptor, ok := authorizer.(grantMessageAcceptor)
	if !ok {
		return ErrAuthorizerCannotAcceptGrants
	}
	acceptor.addGrantUserMessage(userMessage)

	fallback := authorizer.WithoutCodeUnit()
	if fallback == nil || fallback == authorizer {
		return nil
	}
	fallbackAcceptor, ok := fallback.(grantMessageAcceptor)
	if !ok {
		return ErrAuthorizerCannotAcceptGrants
	}
	fallbackAcceptor.addGrantUserMessage(userMessage)
	return nil
}

var readGrantToolNames = []string{"read_file", "ls"}

func toolAllowsReadGrants(toolName string) bool {
	return slicesContains(readGrantToolNames, toolName)
}

func slicesContains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

type grantStore struct {
	mu           sync.RWMutex
	userMessages []string
}

func newGrantStore() *grantStore {
	return &grantStore{}
}

func (g *grantStore) addGrantUserMessage(userMessage string) {
	if userMessage == "" {
		return
	}
	g.mu.Lock()
	g.userMessages = append(g.userMessages, userMessage)
	g.mu.Unlock()
}

func (g *grantStore) isGrantedForRead(sandboxDir string, absPath string, toolName string, allowOutsideSandbox bool) bool {
	if g == nil || !toolAllowsReadGrants(toolName) {
		return false
	}
	if !allowOutsideSandbox && !withinSandbox(sandboxDir, absPath) {
		return false
	}

	g.mu.RLock()
	messages := append([]string(nil), g.userMessages...)
	g.mu.RUnlock()

	relPath := ""
	if sandboxDir != "" {
		if rel, err := filepath.Rel(sandboxDir, absPath); err == nil && rel != "." {
			relPath = rel
		}
	}

	candidates := grantCandidates(absPath, relPath)
	for _, msg := range messages {
		for _, cand := range candidates {
			if cand == "" {
				continue
			}
			if !messageHasGrantPrefix(msg, cand) {
				continue
			}
			if g.exactGrantAllowsPath(sandboxDir, absPath, cand, allowOutsideSandbox) {
				return true
			}
		}

		if g.globGrantAllowsPath(absPath, relPath, msg, allowOutsideSandbox, sandboxDir) {
			return true
		}
	}

	return false
}

// grantCandidates returns a set of candidate grant strings that we will try to match against a user message.
//
// Inputs:
//   - absPath: absolute filesystem path for the requested read (ex: "/repo/sandbox/docs/readme.md")
//   - relPath: path to the same target relative to sandboxDir, if available (ex: "docs/readme.md"); "" if filepath.Rel errors.
//
// Output:
//   - A de-duplicated list including absPath, its ancestor directories, relPath, and relPath's ancestor directories,
//     plus a "./"+relPath form when relPath is non-empty.
//
// Examples (sandboxDir="/repo/sandbox"):
//   - absPath="/repo/sandbox/docs/readme.md", relPath="docs/readme.md" yields candidates including:
//     "/repo/sandbox/docs/readme.md", "/repo/sandbox/docs", "/repo/sandbox", "docs/readme.md", "docs", "./docs/readme.md".
func grantCandidates(absPath string, relPath string) []string {
	seen := make(map[string]struct{}, 16)
	var out []string

	add := func(candidate string) {
		if candidate == "" || candidate == "." {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}

	add(absPath)
	for dir := filepath.Dir(absPath); dir != absPath && dir != ""; dir = filepath.Dir(dir) {
		add(dir)
		if isFilesystemRoot(dir) {
			break
		}
	}

	if relPath != "" && relPath != "." {
		add(relPath)
		sep := string(filepath.Separator)
		if !strings.HasPrefix(relPath, ".") {
			add("." + sep + relPath)
		}

		for dir := filepath.Dir(relPath); dir != "." && dir != relPath && dir != ""; dir = filepath.Dir(dir) {
			add(dir)
			if isFilesystemRoot(dir) {
				break
			}
		}
	}

	return out
}

func (g *grantStore) exactGrantAllowsPath(sandboxDir string, absPath string, grant string, allowOutsideSandbox bool) bool {
	grantAbs := grant
	if !filepath.IsAbs(grantAbs) {
		grantAbs = filepath.Join(sandboxDir, grantAbs)
	}
	grantAbs = filepath.Clean(grantAbs)

	if isFilesystemRoot(grantAbs) {
		return false
	}
	if !allowOutsideSandbox && !withinSandbox(sandboxDir, grantAbs) {
		return false
	}

	info, err := os.Stat(grantAbs)
	if err != nil {
		return false
	}

	if info.IsDir() {
		return withinSandbox(grantAbs, absPath)
	}
	return grantAbs == absPath
}

func (g *grantStore) globGrantAllowsPath(absPath string, relPath string, message string, allowOutsideSandbox bool, sandboxDir string) bool {
	if !allowOutsideSandbox && !withinSandbox(sandboxDir, absPath) {
		return false
	}

	for _, pattern := range extractGrantPatterns(message) {
		if pattern == "" || !containsGlobMetachar(pattern) {
			continue
		}

		if !filepath.IsAbs(pattern) && relPath == "" {
			continue
		}

		target := relPath
		if filepath.IsAbs(pattern) {
			target = absPath
		}

		ok, err := filepath.Match(pattern, target)
		if err != nil || !ok {
			continue
		}

		return true
	}
	return false
}

func filterGrantedReadPaths(grants *grantStore, sandboxDir string, toolName string, allowOutsideSandbox bool, paths []string) []string {
	if grants == nil || len(paths) == 0 {
		return paths
	}

	out := paths[:0]
	for _, path := range paths {
		if grants.isGrantedForRead(sandboxDir, path, toolName, allowOutsideSandbox) {
			continue
		}
		out = append(out, path)
	}
	return out
}

func containsGlobMetachar(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

func extractGrantPatterns(message string) []string {
	var patterns []string
	for i := 0; i < len(message); i++ {
		if message[i] != '@' {
			continue
		}

		start := i + 1
		if start >= len(message) {
			continue
		}

		var token string
		if message[start] == '"' {
			start++
			if start >= len(message) {
				continue
			}
			end := strings.IndexByte(message[start:], '"')
			if end == -1 {
				token = message[start:]
				i = len(message) - 1
			} else {
				token = message[start : start+end]
				i = start + end
			}
		} else {
			end := start
			for end < len(message) && !isGrantTokenTerminator(message[end]) {
				end++
			}
			token = message[start:end]
			i = end
		}

		token = strings.TrimSpace(token)
		token = strings.TrimRight(token, ".,;:!?)]}\"'")
		if token == "" || token == string(filepath.Separator) {
			continue
		}
		patterns = append(patterns, token)
	}
	return patterns
}

func isGrantTokenTerminator(b byte) bool {
	if b <= ' ' {
		return true
	}
	switch b {
	case ',', ';', ':', ')', ']', '}', '"':
		return true
	default:
		return false
	}
}

func messageHasGrantPrefix(message string, prefix string) bool {
	for i := 0; i < len(message); i++ {
		if message[i] != '@' {
			continue
		}

		start := i + 1
		if start >= len(message) {
			continue
		}

		if message[start] == '"' {
			start++
		}
		if start+len(prefix) > len(message) {
			continue
		}
		if message[start:start+len(prefix)] != prefix {
			continue
		}

		after := start + len(prefix)
		if after >= len(message) {
			return true
		}

		next := message[after]
		if isGrantBoundary(next) {
			return true
		}

		if next == '/' || next == '\\' {
			if after+1 >= len(message) {
				return true
			}
			if isGrantBoundary(message[after+1]) {
				return true
			}
		}
	}
	return false
}

func isGrantBoundary(b byte) bool {
	if b <= ' ' {
		return true
	}
	switch b {
	case ',', '.', ';', ':', '!', '?', ')', ']', '}', '"', '`', '\'':
		return true
	default:
		return false
	}
}

func isFilesystemRoot(path string) bool {
	clean := filepath.Clean(path)
	return filepath.Dir(clean) == clean
}
