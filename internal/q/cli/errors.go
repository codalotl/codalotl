package cli

import "fmt"

// ExitCoder is an error with an explicit process exit code.
type ExitCoder interface {
	error
	ExitCode() int
}

// UsageError indicates a user-facing mistake (exit code 2).
type UsageError struct {
	Message string
}

func (e UsageError) Error() string { return e.Message }
func (e UsageError) ExitCode() int { return 2 }

func usageErrorf(format string, args ...any) UsageError {
	return UsageError{Message: fmt.Sprintf(format, args...)}
}

// ExitError wraps an error with a specific exit code.
type ExitError struct {
	Code int
	Err  error
}

func (e ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit code %d", e.Code)
	}
	return e.Err.Error()
}

func (e ExitError) Unwrap() error { return e.Err }
func (e ExitError) ExitCode() int { return e.Code }

