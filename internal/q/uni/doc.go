// Package uni provides Unicode helpers for terminal text layout and text segmentation.
//
// It measures monospace terminal display widths for strings, byte slices, and runes, and it iterates grapheme clusters with byte offsets. Width calculations default
// to non-East Asian rules when no Options value is provided.
package uni
