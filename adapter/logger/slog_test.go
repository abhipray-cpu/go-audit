package logger_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/abhipray-cpu/go-audit/adapter/logger"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time interface check.
var _ port.LoggerPort = (*logger.SlogAdapter)(nil)

// newTestLogger returns a SlogAdapter whose output is captured in buf.
// Uses JSON handler at Debug level so all levels are visible.
func newTestLogger() (*logger.SlogAdapter, *bytes.Buffer) {
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	l := slog.New(h)
	return logger.NewSlog(l), &buf
}

func TestSlogAdapter_Levels(t *testing.T) {
	tests := []struct {
		name  string
		call  func(port.LoggerPort)
		level string
	}{
		{"Info", func(l port.LoggerPort) { l.Info("info msg") }, "INFO"},
		{"Warn", func(l port.LoggerPort) { l.Warn("warn msg") }, "WARN"},
		{"Error", func(l port.LoggerPort) { l.Error("error msg") }, "ERROR"},
		{"Debug", func(l port.LoggerPort) { l.Debug("debug msg") }, "DEBUG"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter, buf := newTestLogger()
			tt.call(adapter)
			output := buf.String()
			if !strings.Contains(output, tt.level) {
				t.Errorf("expected level %q in output: %s", tt.level, output)
			}
		})
	}
}

func TestSlogAdapter_WithFields(t *testing.T) {
	adapter, buf := newTestLogger()

	adapter.Info("user action",
		"user_id", "u-123",
		"action", "login",
		"attempt", 3,
	)

	output := buf.String()
	for _, want := range []string{"user_id", "u-123", "action", "login", "attempt"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output: %s", want, output)
		}
	}
}

func TestSlogAdapter_NilLogger(t *testing.T) {
	// NewSlog(nil) should fall back to slog.Default() — no panic.
	adapter := logger.NewSlog(nil)
	adapter.Info("hello") // smoke test, must not panic
}
