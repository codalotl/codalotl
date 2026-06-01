package health

import "log/slog"

// Ctx carries a logger for health logging convenience methods. A zero-value Ctx is usable; logging is skipped when Logger is nil.
type Ctx struct {
	Logger *slog.Logger // Logger receives health log entries; nil disables logging.
}

// NewCtx returns a Ctx that uses logger for health log entries. Passing nil returns a usable Ctx that skips logging.
func NewCtx(logger *slog.Logger) Ctx {
	return Ctx{Logger: logger}
}

// LogNewErr creates a new health error, logs it with c.Logger, and returns it. The args use slog's key/value or slog.Attr format; if c.Logger is nil, the error
// is returned without being logged.
func (c Ctx) LogNewErr(msg string, args ...any) error {
	return LogNewErr(c.Logger, msg, args...)
}

// LogWrappedErr creates a new health error that wraps wrapped, logs it with c.Logger, and returns it. The args use slog's key/value or slog.Attr format; if c.Logger
// is nil, the error is returned without being logged. Pass a non-nil wrapped error to preserve the original cause.
func (c Ctx) LogWrappedErr(msg string, wrapped error, args ...any) error {
	return LogWrappedErr(c.Logger, msg, wrapped, args...)
}

// Log writes msg and args to c.Logger at info level. The args use slog's key/value or slog.Attr format; if c.Logger is nil, Log does nothing.
func (c Ctx) Log(msg string, args ...any) {
	if c.Logger != nil {
		c.Logger.Info(msg, args...)
	}
}
