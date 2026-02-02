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

// Available returns true if the clipboard is available on this system.
func Available() bool
```