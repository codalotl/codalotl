package gocode

import "fmt"

// extractPackageName returns the package identifier from the first package declaration in src.
//
// Caveat: it does not support comments or newlines between the package keyword and the package name. Specifically, both // and /* */ comments in that position are
// rejected, and a package clause split across lines is not recognized (a space or tab must follow "package"). Additionally, only ASCII letters, digits, and '_'
// are accepted in the package name; Unicode identifiers are not supported.
func extractPackageName(src []byte) (string, error) {
	const keyword = "package"

	i, n := 0, len(src)

	// --- strip UTF‑8 BOM ----------------------------------------------------
	if n >= 3 && src[0] == 0xEF && src[1] == 0xBB && src[2] == 0xBF {
		i = 3
	}

	// helper lambdas (all inlined by compiler)
	isSpace := func(b byte) bool { return b == ' ' || b == '\t' || b == '\r' || b == '\n' }
	isLetter := func(b byte) bool { return b == '_' || ('A' <= b && b <= 'Z') || ('a' <= b && b <= 'z') }
	isDigit := func(b byte) bool { return '0' <= b && b <= '9' }

	for i < n {
		// skip whitespace:
		for i < n && isSpace(src[i]) {
			i++
		}
		if i >= n {
			break
		}

		// line comments ("//"):
		if src[i] == '/' && i+1 < n && src[i+1] == '/' {
			for i < n && src[i] != '\n' {
				i++
			}
			continue
		}

		// block comment ("/* … */"):
		if src[i] == '/' && i+1 < n && src[i+1] == '*' {
			i += 2
			for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			if i+1 >= n {
				return "", fmt.Errorf("unterminated block comment before package declaration")
			}
			i += 2
			continue
		}

		// first real token should be "package":
		if i+len(keyword) > n {
			return "", fmt.Errorf("first code line is not a package declaration")
		}
		for k := 0; k < len(keyword); k++ {
			if src[i+k] != keyword[k] {
				return "", fmt.Errorf("first code line is not a package declaration")
			}
		}
		j := i + len(keyword)

		// require at least one space / tab right after "package"
		if j >= n || !(src[j] == ' ' || src[j] == '\t') {
			return "", fmt.Errorf(`"package" keyword must be followed by space`)
		}
		for j < n && (src[j] == ' ' || src[j] == '\t') {
			j++
		}

		// parse identifier:
		start := j
		if start >= n || !isLetter(src[start]) {
			return "", fmt.Errorf("invalid or missing package name")
		}
		j++
		for j < n && (isLetter(src[j]) || isDigit(src[j])) {
			j++
		}
		end := j

		// ignore trailing junk to end‑of‑line:
		for j < n && src[j] != '\n' {
			if src[j] == '/' && j+1 < n && src[j+1] == '/' { // comment
				break
			}
			if src[j] == ';' { // optional semicolon
				break
			}
			if !isSpace(src[j]) {
				return "", fmt.Errorf("unexpected token after package name")
			}
			j++
		}

		return string(src[start:end]), nil
	}

	return "", fmt.Errorf("no package declaration found")
}
