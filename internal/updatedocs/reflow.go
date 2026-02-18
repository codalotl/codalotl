package updatedocs

import (
	"bytes"
	"fmt"
	"go/format"
	"strings"
)

type reflowGroupKind int

const (
	reflowGroupKindNewline reflowGroupKind = iota
	reflowGroupKindParagraph
	reflowGroupKindCode
	reflowGroupKindList
	reflowGroupKindListItem
	reflowGroupKindNumberedList
	reflowGroupKindPragma
)

// String returns a string representation of the reflowGroupKind.
func (r reflowGroupKind) String() string {
	switch r {
	case reflowGroupKindNewline:
		return "newline"
	case reflowGroupKindParagraph:
		return "paragraph"
	case reflowGroupKindCode:
		return "code"
	case reflowGroupKindList:
		return "list"
	case reflowGroupKindListItem:
		return "listItem"
	case reflowGroupKindNumberedList:
		return "numberedList"
	case reflowGroupKindPragma:
		return "pragma"
	default:
		return "unknown"
	}
}

type reflowGroup struct {
	kind      reflowGroupKind
	lines     []string      // raw lines input
	text      string        // only for paragraph and list item: the words without "//" and no newlines
	listItems []reflowGroup // all groups here must be reflowGroupKindListItem
}

// reflowDocComment reflows "//" doc comments to fit within softMaxCols columns, adding or removing newlines so that each line is approximately softMaxCols cols.
// Lines may exceed softMaxCols if there's a long word or inline `code snippet`. Preserves multiple paragraphs and code blocks; preserves list structure while reflowing
// list item text to fit the width.
//
// Output format: gofmt-style comment lines with indentLevel leading tabs.
//   - Paragraph lines: indent + "// " + text
//   - Blank lines: indent + "//"
//   - Code lines: indent + the original gofmt prefix (e.g., "//\t...")
//   - Bulleted list items: indent + `//   - ` + text (and continuation lines if necessary)
//   - Numbered list items: indent + `//  N. ` + text (and continuation lines if necessary)
//
// All lines are newline-terminated. Returns input unchanged if not a valid "//" comment.
func reflowDocComment(doc string, indentLevel int, spacesPerTab int, softMaxCols int) string {
	trimmedDoc := strings.TrimSpace(doc)

	// Handle empty input
	if trimmedDoc == "" {
		return ""
	}

	// Sanitize doc by stripping whitespace per line
	lines := strings.Split(trimmedDoc, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}

	// Make sure it's a comment we can deal with: each line starts with "//"
	// This lets /* */ comments exist as-is.
	for _, line := range lines {
		if line != "" && !strings.HasPrefix(line, "//") {
			return doc // Not a valid doc comment format, return as-is
		}
	}

	// Put code in gofmt approved format.
	formattedDoc, err := formatDocComment(lines)
	if err != nil {
		return doc // If formatting fails, return original
	}
	if formattedDoc == "\n" {
		return ""
	}

	// Break doc back into lines
	lines = strings.Split(strings.TrimSpace(formattedDoc), "\n")

	// Put the lines into groups according to their type
	groups := groupDocLines(lines)

	// For each group, calculate its text or listItems. For each listItem, calculate its text.
	for i := range groups {
		calculateGroupText(&groups[i])
	}

	// DO NOT REMOVE
	// for _, g := range groups {
	// 	fmt.Printf("group: %v, lines: %d, text: %q, listItems: %d\n", g.kind, len(g.lines), g.text, len(g.listItems))
	// 	for i, line := range g.lines {
	// 		fmt.Printf("  line[%d]: %q\n", i, line)
	// 	}
	// 	if len(g.listItems) > 0 {
	// 		fmt.Printf("  listItems:\n")
	// 		for i, item := range g.listItems {
	// 			fmt.Printf("    [%d]: text=%q, lines=%d\n", i, item.text, len(item.lines))
	// 		}
	// 	}
	// 	fmt.Println("----")
	// }

	// Now that we have a sequence of groups, we just need to output them to a new comment.
	//   - break paragraphs by spaces. Be sure not to break inline code (ex: `fmt.Println("hello world")`)
	//   - break list items by spaces
	//   - keep newlines as is
	//   - keep code as is, don't break it.

	var result []string
	indent := strings.Repeat("\t", indentLevel)

	for i, group := range groups {
		switch group.kind {
		case reflowGroupKindNewline:
			result = append(result, indent+"//")

		case reflowGroupKindParagraph:
			if group.text != "" {
				lines := reflowTextPreservingInlineCode(group.text, softMaxCols-indentLevel*spacesPerTab-3) // -3 for "// "
				for _, line := range lines {
					result = append(result, indent+"// "+line)
				}
			}

		case reflowGroupKindCode:
			for _, line := range group.lines {
				result = append(result, indent+line)
			}

		case reflowGroupKindList:
			for _, listItem := range group.listItems {
				if listItem.text != "" {
					availableWidth := softMaxCols - indentLevel*spacesPerTab - 6 // -6 for "//   - "
					lines := reflowTextPreservingInlineCode(listItem.text, availableWidth)
					if len(lines) > 0 {
						// First line with bullet
						result = append(result, indent+"//   - "+lines[0])
						// Continuation lines
						for _, line := range lines[1:] {
							result = append(result, indent+"//     "+line)
						}
					}
				}
			}
			// Ensure a blank line separates the list from a following paragraph when missing
			if i+1 < len(groups) && groups[i+1].kind == reflowGroupKindParagraph {
				result = append(result, indent+"//")
			}
		case reflowGroupKindNumberedList:
			for itemIndex, listItem := range group.listItems {
				if listItem.text != "" {
					// Calculate prefix length based on item number: "//  1. ", "//  10. ", etc.
					prefixLength := len(fmt.Sprintf("//  %d. ", itemIndex+1))
					availableWidth := softMaxCols - indentLevel*spacesPerTab - prefixLength
					lines := reflowTextPreservingInlineCode(listItem.text, availableWidth)
					if len(lines) > 0 {
						// First line with number
						result = append(result, indent+fmt.Sprintf("//  %d. %s", itemIndex+1, lines[0]))
						// Continuation lines
						for _, line := range lines[1:] {
							result = append(result, indent+"//     "+line)
						}
					}
				}
			}
			// Ensure a blank line separates the list from a following paragraph when missing
			if i+1 < len(groups) && groups[i+1].kind == reflowGroupKindParagraph {
				result = append(result, indent+"//")
			}
		case reflowGroupKindPragma:
			for _, line := range group.lines {
				// Keep pragma lines exactly as-is
				result = append(result, indent+line)
			}
		}
	}

	return strings.Join(result, "\n") + "\n"
}

// formatDocComment uses go/format to clean up a doc comment and put it in gofmt-approved format. It takes the comment lines and returns the formatted comment as
// a string, newline-terminated.
func formatDocComment(lines []string) (string, error) {
	if len(lines) == 0 {
		return "\n", nil
	}

	// Create a temporary Go source with the comment
	var buf bytes.Buffer
	buf.WriteString("package main\n\n")

	// Write the comment lines
	for _, line := range lines {
		buf.WriteString(line + "\n")
	}

	// Add a dummy function so the comment has something to attach to
	buf.WriteString("func dummy() {}\n")

	// Format the source
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return "", err
	}

	// Extract just the comment part
	formattedStr := string(formatted)
	formattedLines := strings.Split(formattedStr, "\n")

	// Find the comment lines - skip package declaration and collect consecutive comment lines
	var commentLines []string
	inComment := false
	for _, line := range formattedLines {
		if strings.HasPrefix(line, "//") {
			inComment = true
			commentLines = append(commentLines, line)
		} else if inComment && line != "" {
			// Hit a non-comment, non-empty line after seeing comments - we're done
			break
		}
		// Continue if we haven't started collecting comments yet or if it's an empty line within comments
	}

	return strings.Join(commentLines, "\n") + "\n", nil
}

func wrapWords(words []string, width int) []string {
	if len(words) == 0 {
		return nil
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		// If current line already meets or exceeds width, start new line
		if len(line) >= width {
			lines = append(lines, line)
			line = w
		} else {
			// Special case: if line is nearly full (75% of width) and adding word
			// would make it way too long (150% of width), start new line instead
			nearlyFull := len(line) >= int(float64(width)*0.75)
			wouldBeTooLong := len(line)+1+len(w) > int(float64(width)*1.5)

			if nearlyFull && wouldBeTooLong {
				lines = append(lines, line)
				line = w
			} else {
				// Add word to current line (even if it makes line exceed width)
				line += " " + w
			}
		}
	}
	lines = append(lines, line)

	return lines
}

func groupDocLines(lines []string) []reflowGroup {

	// Notes on how gofmt handles lists and code.
	//   - first, note that lines is preformatted with gofmt, so it collapses various states into valid gofmt
	//   - proper bulleted lists start with "//   -" -- ie, 3 spaces and transform all bullet types into "-"
	//   - unindented bulleted lists like "// - item" are not considered bulleted lists by gofmt. They're just normal comment.
	//   - Numbered lists start with "//  1." -- ie, 2 spaces followed by number followed by period.
	//   - Code is "//\tcode" -- ie, just a tab char after the slashes.
	//   - Gofmt will convert "indented" bulleted lists (regardless of tabs or spaces count, as long as indented), into the proper 3 spaces kind.
	//     - same for indented numbered lists, but 2 spaces.
	//     - In other words, if a "code block" starts with a bullet or number and "." or ")", it gets list-ized and is no longer code.
	//   - gofmt will also often add blank lines (just "//") as separators between lists/code and other comments. This is fine, we just need to preserve these seperators.

	// BUG (minor): If we make an improper list into a proper list, gofmt may add extra blank lines ("//"). I believe that currently because of how reflow is called, the code is gofmt anyway afterwards.

	var groups []reflowGroup
	i := 0

	for i < len(lines) {
		line := lines[i]

		// Check for blank line
		if line == "//" {
			groups = append(groups, reflowGroup{
				kind:  reflowGroupKindNewline,
				lines: []string{line},
			})
			i++
			continue
		}

		// Check for pragma/directive lines that must be preserved exactly (//go:..., // +build, // #cgo, //nolint, etc.)
		if strings.HasPrefix(line, "//") && shouldPreserveComment(line) {
			pragmaGroup := reflowGroup{
				kind:  reflowGroupKindPragma,
				lines: []string{line},
			}
			i++
			groups = append(groups, pragmaGroup)
			continue
		}

		// Check for code (starts with "//\t")
		if strings.HasPrefix(line, "//\t") {
			codeGroup := reflowGroup{
				kind:  reflowGroupKindCode,
				lines: []string{line},
			}
			i++
			// Collect consecutive code lines
			for i < len(lines) && strings.HasPrefix(lines[i], "//\t") {
				codeGroup.lines = append(codeGroup.lines, lines[i])
				i++
			}
			groups = append(groups, codeGroup)
			continue
		}

		// Check for proper list items (start with "//   -")
		if strings.HasPrefix(line, "//   -") {
			listGroup := reflowGroup{
				kind:      reflowGroupKindList,
				listItems: []reflowGroup{},
			}

			// Collect all list items in this list
			for i < len(lines) && (strings.HasPrefix(lines[i], "//   -") || strings.HasPrefix(lines[i], "//     ")) {
				if strings.HasPrefix(lines[i], "//   -") {
					// Start of new list item
					listItem := reflowGroup{
						kind:  reflowGroupKindListItem,
						lines: []string{lines[i]},
					}
					i++
					// Collect continuation lines for this list item
					for i < len(lines) && strings.HasPrefix(lines[i], "//     ") {
						listItem.lines = append(listItem.lines, lines[i])
						i++
					}
					listGroup.listItems = append(listGroup.listItems, listItem)
				} else {
					i++ // Skip unexpected continuation line
				}
			}
			groups = append(groups, listGroup)
			continue
		}

		// Check for improper list items (start with "// -", "// *", "// +", or bullet unicode)
		if isImproperListItem(line) {
			listGroup := reflowGroup{
				kind:      reflowGroupKindList,
				listItems: []reflowGroup{},
			}

			// Collect consecutive improper list items
			for i < len(lines) && isImproperListItem(lines[i]) {
				listItem := reflowGroup{
					kind:  reflowGroupKindListItem,
					lines: []string{lines[i]},
				}
				listGroup.listItems = append(listGroup.listItems, listItem)
				i++
			}
			groups = append(groups, listGroup)
			continue
		}

		// Check for proper numbered list items (start with "//  1.", "//  2.", etc.)
		if isProperNumberedListItem(line) {
			numberedListGroup := reflowGroup{
				kind:      reflowGroupKindNumberedList,
				listItems: []reflowGroup{},
			}

			// Collect all numbered list items in this list
			for i < len(lines) && (isProperNumberedListItem(lines[i]) || strings.HasPrefix(lines[i], "//     ")) {
				if isProperNumberedListItem(lines[i]) {
					// Start of new numbered list item
					listItem := reflowGroup{
						kind:  reflowGroupKindListItem,
						lines: []string{lines[i]},
					}
					i++
					// Collect continuation lines for this list item
					for i < len(lines) && strings.HasPrefix(lines[i], "//     ") {
						listItem.lines = append(listItem.lines, lines[i])
						i++
					}
					numberedListGroup.listItems = append(numberedListGroup.listItems, listItem)
				} else {
					i++ // Skip unexpected continuation line
				}
			}
			groups = append(groups, numberedListGroup)
			continue
		}

		// Check for improper numbered list items (start with "// 1.", "// 2.", etc.)
		if isImproperNumberedListItem(line) {
			numberedListGroup := reflowGroup{
				kind:      reflowGroupKindNumberedList,
				listItems: []reflowGroup{},
			}

			// Collect consecutive improper numbered list items
			for i < len(lines) && isImproperNumberedListItem(lines[i]) {
				listItem := reflowGroup{
					kind:  reflowGroupKindListItem,
					lines: []string{lines[i]},
				}
				numberedListGroup.listItems = append(numberedListGroup.listItems, listItem)
				i++
			}
			groups = append(groups, numberedListGroup)
			continue
		}

		// Default: paragraph text
		paragraphGroup := reflowGroup{
			kind:  reflowGroupKindParagraph,
			lines: []string{line},
		}
		i++
		// Collect consecutive non-special lines for this paragraph
		for i < len(lines) && !isSpecialLine(lines[i]) {
			paragraphGroup.lines = append(paragraphGroup.lines, lines[i])
			i++
		}
		groups = append(groups, paragraphGroup)
	}

	return groups
}

// isImproperListItem checks if a line is an improper list item (// -, // *, // +, or bullet Unicode).
func isImproperListItem(line string) bool {
	if strings.HasPrefix(line, "// - ") || strings.HasPrefix(line, "// * ") || strings.HasPrefix(line, "// + ") {
		return true
	}
	// Check for bullet unicode (common bullets: •, ‣, ▸, ▪, ▫, ◦, ⁃)
	if strings.HasPrefix(line, "// ") && len(line) > 3 {
		text := line[3:]
		if len(text) > 0 {
			runes := []rune(text)
			if len(runes) > 0 {
				char := runes[0]
				return char == '•' || char == '‣' || char == '▸' || char == '▪' || char == '▫' || char == '◦' || char == '⁃'
			}
		}
	}
	return false
}

// isProperNumberedListItem checks if a line is a proper numbered list item (`//  1. `, `//  2. `, etc.).
func isProperNumberedListItem(line string) bool {
	if !strings.HasPrefix(line, "//  ") || len(line) < 6 {
		return false
	}
	// Extract the part after "//  "
	text := line[4:]
	// Look for pattern: number followed by period and space
	for i, char := range text {
		if char >= '0' && char <= '9' {
			continue
		} else if char == '.' && i > 0 && i+1 < len(text) && text[i+1] == ' ' {
			return true
		} else {
			return false
		}
	}
	return false
}

// isImproperNumberedListItem checks if a line is an improper numbered list item (`// 1. `, `// 2. `, etc.).
func isImproperNumberedListItem(line string) bool {
	if !strings.HasPrefix(line, "// ") || len(line) < 5 {
		return false
	}
	// Extract the part after "// "
	text := line[3:]
	// Look for pattern: number followed by period and space
	for i, char := range text {
		if char >= '0' && char <= '9' {
			continue
		} else if char == '.' && i > 0 && i+1 < len(text) && text[i+1] == ' ' {
			return true
		} else {
			return false
		}
	}
	return false
}

// isSpecialLine checks if a line is a special type (blank, code, pragmas, or list item).
func isSpecialLine(line string) bool {
	if line == "//" || strings.HasPrefix(line, "//\t") || strings.HasPrefix(line, "//   -") || isImproperListItem(line) || isProperNumberedListItem(line) || isImproperNumberedListItem(line) {
		return true
	}
	// Treat pragma lines as special so they don't get absorbed into paragraphs
	if strings.HasPrefix(line, "//") && shouldPreserveComment(line) {
		return true
	}
	return false
}

// calculateGroupText calculates the text field for paragraph and list item groups.
func calculateGroupText(group *reflowGroup) {
	switch group.kind {
	case reflowGroupKindParagraph:
		var parts []string
		for _, line := range group.lines {
			// Remove "//" prefix and leading whitespace
			text := strings.TrimSpace(strings.TrimPrefix(line, "//"))
			if text != "" {
				parts = append(parts, text)
			}
		}
		// Join with single spaces and collapse any multiple spaces
		group.text = strings.Join(parts, " ")

	case reflowGroupKindListItem:
		var parts []string
		for _, line := range group.lines {
			// Remove "//" prefix and leading whitespace
			text := strings.TrimSpace(strings.TrimPrefix(line, "//"))
			if text != "" {
				// Remove bullet character for list items
				text = removeBulletPrefix(text)
				if text != "" {
					parts = append(parts, text)
				}
			}
		}
		// Join with single spaces and collapse any multiple spaces
		group.text = strings.Join(parts, " ")

	case reflowGroupKindList:
		// Calculate text for each list item
		for i := range group.listItems {
			calculateGroupText(&group.listItems[i])
		}

	case reflowGroupKindNumberedList:
		// Calculate text for each list item
		for i := range group.listItems {
			calculateGroupText(&group.listItems[i])
		}
	}
}

// removeBulletPrefix removes the bullet character or numbered prefix from the beginning of a string.
func removeBulletPrefix(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return s
	}

	// Handle ASCII bullets: -, *, +
	if len(s) > 1 && (s[0] == '-' || s[0] == '*' || s[0] == '+') && s[1] == ' ' {
		return strings.TrimSpace(s[2:])
	}

	// Handle Unicode bullets
	runes := []rune(s)
	if len(runes) > 0 {
		char := runes[0]
		if char == '•' || char == '‣' || char == '▸' || char == '▪' || char == '▫' || char == '◦' || char == '⁃' {
			remaining := string(runes[1:])
			return strings.TrimSpace(remaining)
		}
	}

	// Handle numbered prefixes (1., 2., etc.)
	for i, char := range s {
		if char >= '0' && char <= '9' {
			continue
		} else if char == '.' && i > 0 && i+1 < len(s) && s[i+1] == ' ' {
			return strings.TrimSpace(s[i+2:])
		} else {
			break
		}
	}

	return s
}

// reflowTextPreservingInlineCode soft-wraps text to approximately the specified width while preserving inline code blocks. Inline code blocks (text enclosed in
// backticks) are treated as single units that cannot be broken; lines may exceed the width for long tokens or due to the soft wrap heuristic.
func reflowTextPreservingInlineCode(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}

	// Split text into tokens, preserving inline code blocks
	tokens := tokenizeWithInlineCode(text)
	return wrapWords(tokens, width)
}

// tokenizeWithInlineCode splits text into words while preserving inline code blocks as single tokens.
func tokenizeWithInlineCode(text string) []string {
	// If there are no backticks, just return individual words
	if !strings.Contains(text, "`") {
		return strings.Fields(text)
	}

	// Helper: find next  backtick
	// Returns -1 if none found.
	findNextBacktick := func(runes []rune, start int) int {
		i := start
		for i < len(runes) {
			if runes[i] != '`' {
				i++
				continue
			}
			return i
		}
		return -1
	}

	var tokens []string
	var current strings.Builder
	inCode := false

	runes := []rune(text)
	for i := 0; i < len(runes); {
		r := runes[i]

		if inCode {
			if r == '`' {
				// Close code span; keep it glued to surrounding text by not flushing now
				current.WriteRune('`')
				inCode = false
				i++
				continue
			}
			// Preserve everything inside code, including whitespace
			current.WriteRune(r)
			i++
			continue
		}

		// Outside code
		if r == '`' {
			// Start inline code only if there is a future backtick to close it
			if findNextBacktick(runes, i+1) != -1 {
				// Do not flush current to preserve adjacency like (`code`)
				current.WriteRune('`')
				inCode = true
				i++
				continue
			}
			// Stray backtick: treat as literal
			current.WriteRune('`')
			i++
			continue
		}

		// Whitespace outside code ends the current token
		switch r {
		case ' ', '\t', '\n', '\r', '\f', '\v':
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			i++
			continue
		}

		// Regular character outside code
		current.WriteRune(r)
		i++
	}

	// Flush any remaining content (handles unclosed code as well)
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}
