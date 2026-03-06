package logger

import (
	"log/slog"

	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time interface check.
var _ port.LoggerPort = (*SlogAdapter)(nil)

// SlogAdapter implements LoggerPort using Go's standard log/slog package.
type SlogAdapter struct {
	logger *slog.Logger
}

// NewSlog returns a SlogAdapter wrapping the given slog.Logger.
// If logger is nil, the slog default logger is used.
func NewSlog(logger *slog.Logger) *SlogAdapter {
	if logger == nil {
		logger = slog.Default()
	}
	return &SlogAdapter{logger: logger}
}

// Info logs an informational message.
func (a *SlogAdapter) Info(msg string, args ...any) {
	a.logger.Info(msg, args...)
}

// Warn logs a warning message.
func (a *SlogAdapter) Warn(msg string, args ...any) {
	a.logger.Warn(msg, args...)
}

// Error logs an error message.
func (a *SlogAdapter) Error(msg string, args ...any) {
	a.logger.Error(msg, args...)
}

// Debug logs a debug message.
func (a *SlogAdapter) Debug(msg string, args ...any) {
	a.logger.Debug(msg, args...)
}
