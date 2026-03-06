//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/domain"
)

// ---------------------------------------------------------------------------
// Phase 2 — CTX-001 to CTX-006: Context Metadata Propagation
// ---------------------------------------------------------------------------

// CTX-001: TestContext_ActorPropagation verifies WithActor metadata in version record.
func TestContext_ActorPropagation(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := audit.WithActor(context.Background(), "user-123", domain.ActorHuman)
	rec := versionSync(t, ctx, a, user)

	if rec.Metadata.ActorID != "user-123" {
		t.Fatalf("expected ActorID=user-123, got %q", rec.Metadata.ActorID)
	}
	if rec.Metadata.ActorType != domain.ActorHuman {
		t.Fatalf("expected ActorType=human, got %v", rec.Metadata.ActorType)
	}
}

// CTX-002: TestContext_ReasonPropagation verifies WithReason metadata.
func TestContext_ReasonPropagation(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := audit.WithReason(context.Background(), "quarterly update")
	rec := versionSync(t, ctx, a, user)

	if rec.Metadata.Reason != "quarterly update" {
		t.Fatalf("expected reason='quarterly update', got %q", rec.Metadata.Reason)
	}
}

// CTX-003: TestContext_CorrelationID verifies WithCorrelationID metadata.
func TestContext_CorrelationID(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := audit.WithCorrelationID(context.Background(), "req-abc-123")
	rec := versionSync(t, ctx, a, user)

	if rec.Metadata.CorrelationID != "req-abc-123" {
		t.Fatalf("expected CorrelationID=req-abc-123, got %q", rec.Metadata.CorrelationID)
	}
}

// CTX-004: TestContext_CustomMetadata verifies WithCustomMetadata.
func TestContext_CustomMetadata(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := audit.WithCustomMetadata(context.Background(), "ticket", "JIRA-1234")
	rec := versionSync(t, ctx, a, user)

	if rec.Metadata.Custom["ticket"] != "JIRA-1234" {
		t.Fatalf("expected ticket=JIRA-1234, got %v", rec.Metadata.Custom)
	}
}

// CTX-005: TestContext_MultipleKeys verifies multiple custom metadata keys.
func TestContext_MultipleKeys(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	ctx = audit.WithCustomMetadata(ctx, "key1", "val1")
	ctx = audit.WithCustomMetadata(ctx, "key2", "val2")
	ctx = audit.WithCustomMetadata(ctx, "key3", "val3")
	rec := versionSync(t, ctx, a, user)

	for _, kv := range []struct{ k, v string }{
		{"key1", "val1"}, {"key2", "val2"}, {"key3", "val3"},
	} {
		if rec.Metadata.Custom[kv.k] != kv.v {
			t.Fatalf("expected %s=%s, got %v", kv.k, kv.v, rec.Metadata.Custom[kv.k])
		}
	}
}

// CTX-006: TestContext_NoMetadata verifies empty metadata when nothing is set.
func TestContext_NoMetadata(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	clk := newMockClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Clock = clk
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Bare context — no metadata set.
	rec := versionSync(t, context.Background(), a, user)

	if rec.Metadata.ActorID != "" {
		t.Fatalf("expected empty ActorID, got %q", rec.Metadata.ActorID)
	}
	if rec.Metadata.Reason != "" {
		t.Fatalf("expected empty Reason, got %q", rec.Metadata.Reason)
	}
	if rec.Metadata.CorrelationID != "" {
		t.Fatalf("expected empty CorrelationID, got %q", rec.Metadata.CorrelationID)
	}
}
