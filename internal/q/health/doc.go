// Package health provides structured diagnostic errors and slog-based health logging helpers.
//
// Errors created by this package can carry a diagnostic message, slog attributes, and an optional wrapped cause. Use NewErr or Wrap to create errors, LogErr or
// Ctx methods to log and return them, and HumanErr when the message shown to users should differ from the message written to health logs.
package health
