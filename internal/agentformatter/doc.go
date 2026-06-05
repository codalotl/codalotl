// Package agentformatter renders agent events as terminal-ready text for chat UIs and stdout CLIs.
//
// The main entry point is NewTUIFormatter, which returns a Formatter for rendering agent.Event values. When the requested terminal width is greater than MinTerminalWidth,
// the formatter inserts fixed-width TUI wrapping; otherwise it produces stdout-oriented output that callers may wrap themselves.
//
// The formatter sanitizes display text by expanding tabs and escaping non-line-break control bytes. It can emit ANSI colors and effects for semantic roles such
// as accent, success, error, and action text, or produce unstyled output when configured with PlainText.
//
// Tool events may be rendered from semantic llmstream.Presentation values. RenderPlainTextBlock is available for consumers that need unstyled text from a semantic
// block without using the terminal presentation pipeline.
package agentformatter
