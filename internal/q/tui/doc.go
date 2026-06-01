// Package tui builds cross-platform, full-screen terminal user interfaces.
//
// Programs implement Model: Init performs startup work after raw mode is entered, Update handles terminal events and user messages, and View returns the current
// screen contents. RunTUI creates a session, enters raw and alternate-screen mode, drives the model, and returns when the session quits or is interrupted.
//
// tui requires a real terminal. If configured input or output streams are not TTYs, RunTUI may open the controlling terminal for interactive input and rendering;
// if no usable terminal is available, it returns ErrNoTTY. The package does not provide a degraded non-fullscreen or non-raw-mode fallback.
//
// In raw mode, keystrokes such as Ctrl-C and Ctrl-Z are delivered to Update as KeyEvent values. Applications decide whether those keys should call methods such
// as Quit, Interrupt, or Suspend. Resize, signal, optional mouse, and user-defined messages are also delivered through Update.
//
// RunTUI restores terminal state during normal shutdown and before propagating panics recovered from model callbacks or goroutines started by the TUI. After RunTUI
// returns, the TUI value is inert and cannot be reused.
package tui
