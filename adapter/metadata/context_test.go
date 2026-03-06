package metadata

import (
	"context"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain"
)

func TestContextExtractor_WithFn(t *testing.T) {
	fn := func(_ context.Context) domain.VersionMetadata {
		return domain.VersionMetadata{ActorID: "test-actor", Reason: "test-reason"}
	}
	ext := NewContextExtractor(fn)

	meta := ext.Extract(context.Background())
	if meta.ActorID != "test-actor" {
		t.Errorf("ActorID = %q, want %q", meta.ActorID, "test-actor")
	}
	if meta.Reason != "test-reason" {
		t.Errorf("Reason = %q, want %q", meta.Reason, "test-reason")
	}
}

func TestContextExtractor_NilFn(t *testing.T) {
	ext := NewContextExtractor(nil)
	meta := ext.Extract(context.Background())

	if meta.ActorID != "" {
		t.Errorf("ActorID = %q, want empty", meta.ActorID)
	}
}

func TestContextExtractor_EmptyContext(t *testing.T) {
	// Extraction function that returns zero values for empty context.
	fn := func(_ context.Context) domain.VersionMetadata {
		return domain.VersionMetadata{}
	}
	ext := NewContextExtractor(fn)
	meta := ext.Extract(context.Background())

	if meta.ActorID != "" || meta.Reason != "" || meta.CorrelationID != "" {
		t.Errorf("expected zero-value metadata, got %+v", meta)
	}
}
