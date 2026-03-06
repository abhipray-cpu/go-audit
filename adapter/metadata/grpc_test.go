package metadata

import (
	"context"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain"
)

func TestGRPCExtractor_FromMetadata(t *testing.T) {
	md := GRPCMetadata{
		"x-actor-id":       {"svc-billing"},
		"x-actor-type":     {"service"},
		"x-audit-reason":   {"invoice recalculation"},
		"x-correlation-id": {"corr-xyz"},
		"x-trace-id":       {"abc123"},
		"x-span-id":        {"span456"},
	}

	ctx := WithGRPCMetadata(context.Background(), md)
	ext := NewGRPCExtractor()
	meta := ext.Extract(ctx)

	if meta.ActorID != "svc-billing" {
		t.Errorf("ActorID = %q, want %q", meta.ActorID, "svc-billing")
	}
	if meta.ActorType != domain.ActorService {
		t.Errorf("ActorType = %d, want ActorService", meta.ActorType)
	}
	if meta.Reason != "invoice recalculation" {
		t.Errorf("Reason = %q, want %q", meta.Reason, "invoice recalculation")
	}
	if meta.CorrelationID != "corr-xyz" {
		t.Errorf("CorrelationID = %q, want %q", meta.CorrelationID, "corr-xyz")
	}
	if meta.TraceID != "abc123" {
		t.Errorf("TraceID = %q, want %q", meta.TraceID, "abc123")
	}
	if meta.SpanID != "span456" {
		t.Errorf("SpanID = %q, want %q", meta.SpanID, "span456")
	}
	if meta.Source != "grpc" {
		t.Errorf("Source = %q, want %q", meta.Source, "grpc")
	}
}

func TestGRPCExtractor_NoMetadata(t *testing.T) {
	ext := NewGRPCExtractor()
	meta := ext.Extract(context.Background())

	if meta.ActorID != "" {
		t.Errorf("expected empty ActorID, got %q", meta.ActorID)
	}
}

func TestGRPCExtractor_FallbackRequestID(t *testing.T) {
	md := GRPCMetadata{
		"x-request-id": {"req-999"},
	}

	ctx := WithGRPCMetadata(context.Background(), md)
	ext := NewGRPCExtractor()
	meta := ext.Extract(ctx)

	if meta.CorrelationID != "req-999" {
		t.Errorf("CorrelationID = %q, want %q", meta.CorrelationID, "req-999")
	}
}
