// Package termformat formats terminal text with ANSI styling, color conversion, width-aware block layout, and display sanitization.
//
// It provides Color implementations for no color, ANSI colors, 256-color output, and RGB true color; Style for applying SGR attributes to plain or already formatted
// text; and helpers for measuring, normalizing, cutting, laying out, and overlaying terminal text while accounting for ANSI escape sequences.
package termformat
