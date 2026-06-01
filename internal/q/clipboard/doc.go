// Package clipboard reads and writes text from the system clipboard.
//
// It supports Linux, Windows, and macOS. Use Available to gate clipboard-dependent features; operations may return ErrUnavailable when the current system does not
// provide usable clipboard access.
package clipboard
