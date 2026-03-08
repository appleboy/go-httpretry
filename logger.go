package retry

import "log/slog"

// Logger defines the structured logging interface (slog-compatible)
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// nopLogger provides no-op implementation
type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

// defaultLogger uses slog.Default() which outputs to stderr at INFO level
var defaultLogger Logger = &SlogAdapter{logger: slog.Default()}
