package retry

import "log/slog"

// SlogAdapter adapts log/slog.Logger to the retry.Logger interface
type SlogAdapter struct {
	logger *slog.Logger
}

// NewSlogAdapter creates a slog adapter
func NewSlogAdapter(logger *slog.Logger) *SlogAdapter {
	return &SlogAdapter{logger: logger}
}

func (s *SlogAdapter) Debug(msg string, args ...any) {
	s.logger.Debug(msg, args...)
}

func (s *SlogAdapter) Info(msg string, args ...any) {
	s.logger.Info(msg, args...)
}

func (s *SlogAdapter) Warn(msg string, args ...any) {
	s.logger.Warn(msg, args...)
}

func (s *SlogAdapter) Error(msg string, args ...any) {
	s.logger.Error(msg, args...)
}
