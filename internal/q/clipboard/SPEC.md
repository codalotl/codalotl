# clipboard

This is a minimal, cross-platform library for reading/writing to the clipboard.

## Supported Platforms

- Linux
- Windows
- OSX

## Public API

```go
// Write writes s to the clipboard.
func Write(s string) error

// Read reads from the clipboard and returns the text in it.
func Read() (string, error)

// Available reports whether the clipboard is available on this system.
//
// This is intended as a cheap capability check for gating UI/feature flags.
func Available() bool
```
