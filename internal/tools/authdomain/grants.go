package authdomain

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ErrAuthorizerCannotAcceptGrants is returned when an authorizer cannot record grant messages.
var ErrAuthorizerCannotAcceptGrants = errors.New("authdomain: authorizer cannot accept grants")

// grantMessageAcceptor is implemented by authorizers that can record user messages containing grants.
type grantMessageAcceptor interface {
	// addGrantUserMessage records a raw user message that may contain grant tokens.
	addGrantUserMessage(userMessage string)
}

// AddGrantsFromUserMessage adds grants from userMessage to the authorizer. Grants in userMessage are of the form `@relative/path/to/file`, `@/path/to/file`, or
// `@"path with spaces"`. Note that userMessage is a full message typed by the user to the agent, and may contain no grants, errant `@` signs, bad syntax, commas
// or other punctuation after the grant, and so on. This means AddGrantsFromUserMessage needs to robustly handle anything the user may type, and may not know **at
// the time of calling** what grants are actually being made.
//
// The grants are added to the authorizer as well as its fallback, if present. Note: strict sandbox authorizers never allow grants to authorize reads outside of
// their sandbox dir.
//
// An error is only returned if authorizer is not capable of accepting grants. Any other "errors" simply result in no grants being added (ex: file doesn't exist;
// bad glob format).
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

// grantStore stores user-supplied grant messages and evaluates them lazily during read authorization.
type grantStore struct {
	mu           sync.RWMutex // mu protects userMessages.
	userMessages []string     // userMessages stores raw user messages that may contain grant tokens.
}

func newGrantStore() *grantStore {
	return &grantStore{}
}

// addGrantUserMessage records a non-empty raw user message for lazy grant evaluation.
func (g *grantStore) addGrantUserMessage(userMessage string) {
	if userMessage == "" {
		return
	}
	g.mu.Lock()
	g.userMessages = append(g.userMessages, userMessage)
	g.mu.Unlock()
}

// The isGrantedForRead method reports whether a stored user grant authorizes toolName to read absPath.
//
// The requested path is cleaned and, when relative, resolved against sandboxDir. It returns false for nil stores, tools that do not support read grants, and outside-sandbox
// paths when allowOutsideSandbox is false.
func (g *grantStore) isGrantedForRead(sandboxDir string, absPath string, toolName string, allowOutsideSandbox bool) bool {
	if g == nil || !toolAllowsReadGrants(toolName) {
		return false
	}

	requestAbs := absPath
	if !filepath.IsAbs(requestAbs) {
		if sandboxDir == "" {
			return false
		}
		requestAbs = filepath.Join(sandboxDir, requestAbs)
	}
	requestAbs = filepath.Clean(requestAbs)

	if !allowOutsideSandbox && !withinSandbox(sandboxDir, requestAbs) {
		return false
	}

	g.mu.RLock()
	messages := append([]string(nil), g.userMessages...)
	g.mu.RUnlock()

	relPath := ""
	if sandboxDir != "" {
		if rel, err := filepath.Rel(sandboxDir, requestAbs); err == nil && rel != "." {
			relPath = rel
		}
	}

	for _, msg := range messages {
		if toolName == "ls" && g.globGrantAllowsLsOnSegment(requestAbs, msg, allowOutsideSandbox, sandboxDir) {
			return true
		}
		if g.globGrantAllowsPath(requestAbs, relPath, msg, allowOutsideSandbox, sandboxDir) {
			return true
		}

		// Exact grants apply only to the specific token the user wrote (file or directory).
		// Do not infer implicit directory grants from token prefixes.
		for _, token := range extractGrantPatterns(msg) {
			if token == "" || containsGlobMetachar(token) {
				continue
			}
			if g.exactGrantAllowsPath(sandboxDir, requestAbs, token, allowOutsideSandbox) {
				return true
			}
		}
	}

	return false
}

// exactGrantAllowsPath reports whether a non-glob grant token authorizes absPath.
//
// Relative grants are resolved against sandboxDir. A granted directory authorizes its entire subtree, a granted file authorizes only itself, filesystem root is
// never authorized, and when allowOutsideSandbox is false the grant must stay within sandboxDir. The grant path must exist; stat errors deny the grant.
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

// globGrantAllowsPath reports whether any glob grant in message matches absPath.
//
// Absolute grant patterns match absPath; relative grant patterns match relPath, which should be absPath relative to sandboxDir. Invalid patterns and non-glob tokens
// are ignored, and when allowOutsideSandbox is false absPath must be inside sandboxDir.
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

// globGrantAllowsLsOnSegment reports whether a glob grant in message permits listing absPath so matching entries can be discovered.
//
// The listable directory is the directory segment before the grant's first glob metacharacter. Filesystem root is never listable, and when allowOutsideSandbox is
// false the listable directory must be inside sandboxDir.
func (g *grantStore) globGrantAllowsLsOnSegment(absPath string, message string, allowOutsideSandbox bool, sandboxDir string) bool {
	cleanAbsPath := filepath.Clean(absPath)

	for _, pattern := range extractGrantPatterns(message) {
		if pattern == "" || !containsGlobMetachar(pattern) {
			continue
		}

		listableDir := listableDirForGlobPattern(pattern)
		if listableDir == "" {
			continue
		}

		listableAbs := listableDir
		if !filepath.IsAbs(pattern) {
			listableAbs = filepath.Join(sandboxDir, listableDir)
		}
		listableAbs = filepath.Clean(listableAbs)

		if isFilesystemRoot(listableAbs) {
			continue
		}
		if !allowOutsideSandbox && !withinSandbox(sandboxDir, listableAbs) {
			continue
		}

		if cleanAbsPath == listableAbs {
			return true
		}
	}

	return false
}

func listableDirForGlobPattern(pattern string) string {
	first := strings.IndexAny(pattern, "*?[")
	if first == -1 {
		return ""
	}

	prefix := pattern[:first]
	return filepath.Dir(prefix)
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

// extractGrantPatterns returns grant patterns found in a raw user message. It accepts unquoted @path tokens and quoted @"path with spaces" tokens, trims trailing
// punctuation, and does not validate or stat the returned paths.
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
		token = strings.TrimRight(token, ".,;:!?)]}\"'`")
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
	case ',', ';', ':', ')', ']', '}', '"', '`':
		return true
	default:
		return false
	}
}

func isFilesystemRoot(path string) bool {
	clean := filepath.Clean(path)
	return filepath.Dir(clean) == clean
}
