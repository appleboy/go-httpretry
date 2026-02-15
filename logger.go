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

// slogAdapter wraps slog.Logger to implement our Logger interface
type slogAdapter struct {
	logger *slog.Logger
}

func (l *slogAdapter) Debug(msg string, args ...any) {
	l.logger.Debug(msg, args...)
}

func (l *slogAdapter) Info(msg string, args ...any) {
	l.logger.Info(msg, args...)
}

func (l *slogAdapter) Warn(msg string, args ...any) {
	l.logger.Warn(msg, args...)
}

func (l *slogAdapter) Error(msg string, args ...any) {
	l.logger.Error(msg, args...)
}

// defaultLogger uses slog.Default() which outputs to stderr with INFO level
var defaultLogger Logger = &slogAdapter{logger: slog.Default()}
