package port

// LoggerPort provides structured logging for the library's internal operations.
type LoggerPort interface {
	// Info logs an informational message with optional key-value pairs.
	Info(msg string, args ...any)

	// Warn logs a warning message with optional key-value pairs.
	Warn(msg string, args ...any)

	// Error logs an error message with optional key-value pairs.
	Error(msg string, args ...any)

	// Debug logs a debug message with optional key-value pairs.
	Debug(msg string, args ...any)
}
