package metadata

import (
	"context"
	"net/http"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain"
)

func TestHTTPExtractor_FromRequest(t *testing.T) {
	req, _ := http.NewRequest("POST", "/users", nil)
	req.Header.Set("X-Actor-ID", "user-42")
	req.Header.Set("X-Actor-Type", "human")
	req.Header.Set("X-Audit-Reason", "Updated billing address")
	req.Header.Set("X-Correlation-ID", "corr-abc")
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	ctx := WithHTTPRequest(context.Background(), req)

	ext := NewHTTPExtractor()
	meta := ext.Extract(ctx)

	if meta.ActorID != "user-42" {
		t.Errorf("ActorID = %q, want %q", meta.ActorID, "user-42")
	}
	if meta.ActorType != domain.ActorHuman {
		t.Errorf("ActorType = %d, want ActorHuman", meta.ActorType)
	}
	if meta.Reason != "Updated billing address" {
		t.Errorf("Reason = %q, want %q", meta.Reason, "Updated billing address")
	}
	if meta.CorrelationID != "corr-abc" {
		t.Errorf("CorrelationID = %q, want %q", meta.CorrelationID, "corr-abc")
	}
	if meta.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("TraceID = %q, want %q", meta.TraceID, "4bf92f3577b34da6a3ce929d0e0e4736")
	}
	if meta.SpanID != "00f067aa0ba902b7" {
		t.Errorf("SpanID = %q, want %q", meta.SpanID, "00f067aa0ba902b7")
	}
	if meta.Source != "http" {
		t.Errorf("Source = %q, want %q", meta.Source, "http")
	}
}

func TestHTTPExtractor_NoRequest(t *testing.T) {
	ext := NewHTTPExtractor()
	meta := ext.Extract(context.Background())

	if meta.ActorID != "" {
		t.Errorf("expected empty ActorID, got %q", meta.ActorID)
	}
}

func TestHTTPExtractor_FallbackRequestID(t *testing.T) {
	req, _ := http.NewRequest("GET", "/users", nil)
	req.Header.Set("X-Request-ID", "req-123")

	ctx := WithHTTPRequest(context.Background(), req)
	ext := NewHTTPExtractor()
	meta := ext.Extract(ctx)

	if meta.CorrelationID != "req-123" {
		t.Errorf("CorrelationID = %q, want %q", meta.CorrelationID, "req-123")
	}
}
