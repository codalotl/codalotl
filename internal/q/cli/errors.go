package cli

import "fmt"

// ExitCoder is an error with an explicit process exit code.
type ExitCoder interface {
	error // Error reports the message associated with the exit code.

	// ExitCode returns the process exit code to use for this error.
	ExitCode() int
}

// UsageError indicates a user-facing mistake (exit code 2).
type UsageError struct {
	Message string // Message is the text printed before the relevant command usage.
}

// Error returns e.Message.
func (e UsageError) Error() string { return e.Message }

// ExitCode returns 2, the usage-error exit code.
func (e UsageError) ExitCode() int { return 2 }

func usageErrorf(format string, args ...any) UsageError {
	return UsageError{Message: fmt.Sprintf(format, args...)}
}

// ExitError wraps an error with a specific exit code.
type ExitError struct {
	Code int   // Code is the process exit code to return.
	Err  error // Err is the wrapped error; nil produces a generic exit-code message.
}

// Error returns the wrapped error message, or a generic message containing Code when Err is nil.
func (e ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit code %d", e.Code)
	}
	return e.Err.Error()
}

// Unwrap returns e.Err.
func (e ExitError) Unwrap() error { return e.Err }

// ExitCode returns e.Code.
func (e ExitError) ExitCode() int { return e.Code }
