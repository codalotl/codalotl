package health

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
)

type HealthErr struct {
	Message string
	wrapped error
	attrs   []any // NOTE: i expect to make this exported at some point.
}

// Error satisfies the error interface. All aspects will be serialized to the string: msg, wrapped error, and all attrs.
func (e *HealthErr) Error() string {
	var b strings.Builder
	b.WriteString(e.Message)

	if len(e.attrs) > 0 {
		b.WriteString("[")
		writeAttrs(&b, e.attrs)
		b.WriteString("]")
	}

	if e.wrapped != nil {
		b.WriteString(" via ")
		b.WriteString(e.wrapped.Error())
	}

	return b.String()
}

func (e *HealthErr) Unwrap() error {
	return e.wrapped
}

// NewErr returns a new error (unlogged). args is in the same format as slog's args to Info: they can be key/values, or slog.Attrs.
// NOTE: to wrap an error, use Wrap.
func NewErr(msg string, args ...any) error {
	return &HealthErr{Message: msg, attrs: args}
}

// Wrap returns a new error that wraps `wrapped`.
func Wrap(msg string, wrapped error, args ...any) error {
	if wrapped == nil {
		// NOTE: this is a footgun, but probably don't want to panic. Let's at least use an error to make it more likely the user can fix their code.
		wrapped = errors.New("nil wrapped error. WARNING: you should not call Wrap with a nil error")
	}
	return &HealthErr{Message: msg, wrapped: wrapped, attrs: args}
}

// LogNewErr creates a new error with msg and args, logs it, and returns it.
func LogNewErr(logger *slog.Logger, msg string, args ...any) error {
	return LogErr(logger, NewErr(msg, args...))
}

// LogNewErr creates a new error with msg and args, logs it, and returns it.
func LogWrappedErr(logger *slog.Logger, msg string, wrapped error, args ...any) error {
	return LogErr(logger, Wrap(msg, wrapped, args...))
}

// LogErr logs err to logger (if it's not nil) and returns the error. It enables the pattern of logging and returning an error in one line:
//
//	return health.LogErr(logger, errors.New("myerr")) // log and return a basic error
//	// or...
//	return health.LogErr(logger, health.NewErr("myerr", "errkv", v), "otherkv", 3)
//
// When err is a health error (created via NewWrr or Wrap), it gets special treatment, including:
//   - Logging it's kvs to the logger (these will be logged first, and *then* args -- duplicates are permitted)
//   - Logging the wrapped error with a "via" kv
func LogErr(logger *slog.Logger, err error, args ...any) error {
	if logger == nil || err == nil {
		return err
	}

	// If human error, log the underlying log-optimized version of it:
	humanErr, isHumanErr := err.(*HumanErr)
	if isHumanErr {
		err = &humanErr.HealthErr
	}

	h, isHealthErr := err.(*HealthErr)

	// If err is not a health err, just log err.Error() with args:
	if !isHealthErr {
		logger.Error(err.Error(), args...)
		return err
	}

	// Otherwise, if err is a healthErr special case the logging:
	// The message from the outermost error is used.
	msg := h.Message

	// Combine error attrs, via, and arg attrs.
	allArgs := make([]any, 0, len(h.attrs)+len(args)+1)
	allArgs = append(allArgs, h.attrs...)
	if h.wrapped != nil {
		allArgs = append(allArgs, slog.String("via", h.wrapped.Error()))
	}
	allArgs = append(allArgs, args...)

	logger.Error(msg, allArgs...)
	return err
}

// writeAttrs writes attrs (in the protocol of slog attrs to .Log) to b. Attributes will be written in key=value format, as per the Text handler. Ex: `num=3 str="hi"`.
func writeAttrs(b *strings.Builder, attrs []any) {
	if len(attrs) == 0 {
		return
	}

	opts := &slog.HandlerOptions{
		// Set a level that will always be logged.
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Remove time, level, and message keys.
			if a.Key == slog.TimeKey || a.Key == slog.LevelKey || a.Key == slog.MessageKey {
				return slog.Attr{}
			}
			return a
		},
	}

	// We create a logger that writes to a temporary buffer, so we can capture just the attributes.
	handler := slog.NewTextHandler(&noNewlineWriter{w: b}, opts)
	logger := slog.New(handler)

	// Log with an empty message. The attrs will be formatted.
	logger.Log(context.Background(), slog.LevelDebug, "", attrs...)
}

// noNewlineWriter wraps an io.Writer and strips a single trailing newline
// from p before writing it to the underlying writer.
type noNewlineWriter struct {
	w io.Writer
}

// Write implements io.Writer.
func (n *noNewlineWriter) Write(p []byte) (int, error) {
	if len(p) > 0 && p[len(p)-1] == '\n' {
		// slog.TextHandler makes a single call to Write with a trailing newline.
		// We write all but that last byte.
		// We still report the original length on success, because the caller believes
		// it wrote the entire slice.
		written, err := n.w.Write(p[:len(p)-1])
		if err == nil {
			return len(p), nil
		}
		return written, err

	}
	return n.w.Write(p)
}
