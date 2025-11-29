package health

import "log/slog"

type Ctx struct {
	Logger *slog.Logger
}

func NewCtx(logger *slog.Logger) Ctx {
	return Ctx{Logger: logger}
}

func (c Ctx) LogNewErr(msg string, args ...any) error {
	return LogNewErr(c.Logger, msg, args...)
}

func (c Ctx) LogWrappedErr(msg string, wrapped error, args ...any) error {
	return LogWrappedErr(c.Logger, msg, wrapped, args...)
}

func (c Ctx) Log(msg string, args ...any) {
	if c.Logger != nil {
		c.Logger.Info(msg, args...)
	}
}
