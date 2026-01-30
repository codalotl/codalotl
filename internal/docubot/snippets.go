package docubot

import "strings"

// extractSnippets returns the contents of all fenced code blocks in response, in encounter order. A fenced block starts and ends with a line beginning with "```", with an optional
// language tag. Empty or unterminated blocks are ignored. The text between fences is returned verbatim with line breaks preserved.
func extractSnippets(response string) []string {
	var snippets []string
	lines := strings.Split(response, "\n")
	var currentSnippet strings.Builder
	inSnippet := false

	for _, line := range lines {
		// Check for any code block start (with or without language)
		if strings.HasPrefix(line, "```") {
			if !inSnippet {
				// Starting a new code block
				inSnippet = true
				currentSnippet.Reset()
			} else {
				// Ending the current code block
				inSnippet = false
				if currentSnippet.Len() > 0 {
					snippets = append(snippets, currentSnippet.String())
				}
			}
			continue
		}
		if inSnippet {
			currentSnippet.WriteString(line)
			currentSnippet.WriteString("\n")
		}
	}
	return snippets
}

// unwrapSingleSnippet returns the content of the last non-empty fenced code block in response. If no fenced code blocks (lines starting with "```") are present, or if all fenced code
// blocks are empty, the response is returned unchanged.
//
// This method was written for Anthropic models on 2025/08/10, which had trouble outputting JSON without fences. They sometimes write multiple JSON snippets after reconsidering.
func unwrapSingleSnippet(response string) string {
	responseStripped := strings.TrimSpace(response)

	snippets := extractSnippets(responseStripped)
	if len(snippets) == 0 {
		return response
	}
	return snippets[len(snippets)-1]
}
