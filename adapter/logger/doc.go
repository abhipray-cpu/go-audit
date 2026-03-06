// Package logger provides the slog-based logger adapter for go-audit.
//
// This package implements the LoggerPort using Go's standard log/slog
// package. It is the default logger wired by audit.New().
package logger
