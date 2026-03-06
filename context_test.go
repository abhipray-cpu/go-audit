package audit

import (
	"context"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain"
)

func TestWithActor_RoundTrip(t *testing.T) {
	ctx := WithActor(context.Background(), "user-42", domain.ActorHuman)

	if got := ActorFromCtx(ctx); got != "user-42" {
		t.Errorf("ActorFromCtx = %q, want %q", got, "user-42")
	}
	if got := ActorTypeFromCtx(ctx); got != domain.ActorHuman {
		t.Errorf("ActorTypeFromCtx = %v, want ActorHuman", got)
	}
}

func TestWithActor_ServiceType(t *testing.T) {
	ctx := WithActor(context.Background(), "svc-payment", domain.ActorService)

	if got := ActorTypeFromCtx(ctx); got != domain.ActorService {
		t.Errorf("ActorTypeFromCtx = %v, want ActorService", got)
	}
}

func TestWithReason_RoundTrip(t *testing.T) {
	ctx := WithReason(context.Background(), "quarterly update")

	if got := ReasonFromCtx(ctx); got != "quarterly update" {
		t.Errorf("ReasonFromCtx = %q, want %q", got, "quarterly update")
	}
}

func TestWithCorrelationID_RoundTrip(t *testing.T) {
	ctx := WithCorrelationID(context.Background(), "req-abc-123")

	if got := CorrelationIDFromCtx(ctx); got != "req-abc-123" {
		t.Errorf("CorrelationIDFromCtx = %q, want %q", got, "req-abc-123")
	}
}

func TestWithCustomMetadata_RoundTrip(t *testing.T) {
	ctx := context.Background()
	ctx = WithCustomMetadata(ctx, "ticket", "JIRA-1234")
	ctx = WithCustomMetadata(ctx, "env", "staging")

	meta := MetadataFromCtx(ctx)
	if meta.Custom["ticket"] != "JIRA-1234" {
		t.Errorf("Custom[ticket] = %q, want %q", meta.Custom["ticket"], "JIRA-1234")
	}
	if meta.Custom["env"] != "staging" {
		t.Errorf("Custom[env] = %q, want %q", meta.Custom["env"], "staging")
	}
}

func TestWithCustomMetadata_Overwrite(t *testing.T) {
	ctx := WithCustomMetadata(context.Background(), "k", "v1")
	ctx = WithCustomMetadata(ctx, "k", "v2")

	meta := MetadataFromCtx(ctx)
	if meta.Custom["k"] != "v2" {
		t.Errorf("Custom[k] = %q, want %q", meta.Custom["k"], "v2")
	}
}

func TestMetadataFromCtx_EmptyContext(t *testing.T) {
	meta := MetadataFromCtx(context.Background())

	if meta.ActorID != "" {
		t.Errorf("ActorID = %q, want empty", meta.ActorID)
	}
	if meta.Reason != "" {
		t.Errorf("Reason = %q, want empty", meta.Reason)
	}
	if meta.CorrelationID != "" {
		t.Errorf("CorrelationID = %q, want empty", meta.CorrelationID)
	}
	if meta.Custom != nil {
		t.Errorf("Custom = %v, want nil", meta.Custom)
	}
}

func TestMetadataFromCtx_FullContext(t *testing.T) {
	ctx := context.Background()
	ctx = WithActor(ctx, "u1", domain.ActorSystem)
	ctx = WithReason(ctx, "migration")
	ctx = WithCorrelationID(ctx, "corr-1")
	ctx = WithCustomMetadata(ctx, "source", "api")

	meta := MetadataFromCtx(ctx)
	if meta.ActorID != "u1" {
		t.Errorf("ActorID = %q", meta.ActorID)
	}
	if meta.ActorType != domain.ActorSystem {
		t.Errorf("ActorType = %v", meta.ActorType)
	}
	if meta.Reason != "migration" {
		t.Errorf("Reason = %q", meta.Reason)
	}
	if meta.CorrelationID != "corr-1" {
		t.Errorf("CorrelationID = %q", meta.CorrelationID)
	}
	if meta.Custom["source"] != "api" {
		t.Errorf("Custom[source] = %q", meta.Custom["source"])
	}
}

// ExampleWithActor demonstrates attaching actor metadata to a context.
func ExampleWithActor() {
	ctx := context.Background()
	ctx = WithActor(ctx, "user-123", domain.ActorHuman)
	ctx = WithReason(ctx, "profile update")

	meta := MetadataFromCtx(ctx)
	_ = meta // meta.ActorID == "user-123", meta.Reason == "profile update"
	// Output:
}
