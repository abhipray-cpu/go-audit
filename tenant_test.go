package audit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

func TestMultiTenancy_TenantIsolation(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	// Tenant A creates a version.
	ctxA := WithTenant(context.Background(), "tenant-a")
	ctxA = WithCustomMetadata(ctxA, "tenant_id", "tenant-a")
	userA := &testUser{ID: "u1", Name: "Alice", Email: "alice@a.com"}
	p1, err := a.Version(ctxA, userA, port.WithSync())
	if err != nil {
		t.Fatalf("Version (tenant A): %v", err)
	}
	p1.Wait(ctxA)

	// Tenant B creates a version for a different user.
	ctxB := WithTenant(context.Background(), "tenant-b")
	ctxB = WithCustomMetadata(ctxB, "tenant_id", "tenant-b")
	userB := &testUser{ID: "u2", Name: "Bob", Email: "bob@b.com"}
	p2, err := a.Version(ctxB, userB, port.WithSync())
	if err != nil {
		t.Fatalf("Version (tenant B): %v", err)
	}
	p2.Wait(ctxB)

	// Create a tenant-scoped reader.
	scoped := NewTenantScopedReader(mw.reader)

	// Tenant A can see u1.
	rec, err := scoped.GetLatest(ctxA, "testuser", "u1")
	if err != nil {
		t.Fatalf("Tenant A GetLatest u1: %v", err)
	}
	if rec.EntityID != "u1" {
		t.Errorf("expected u1, got %s", rec.EntityID)
	}

	// Tenant A cannot see u2 (belongs to tenant B).
	_, err = scoped.GetLatest(ctxA, "testuser", "u2")
	if err == nil {
		t.Fatal("expected error when tenant A reads tenant B's data")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}

	// Tenant B can see u2.
	rec, err = scoped.GetLatest(ctxB, "testuser", "u2")
	if err != nil {
		t.Fatalf("Tenant B GetLatest u2: %v", err)
	}
	if rec.EntityID != "u2" {
		t.Errorf("expected u2, got %s", rec.EntityID)
	}

	// Tenant B cannot see u1.
	_, err = scoped.GetLatest(ctxB, "testuser", "u1")
	if err == nil {
		t.Fatal("expected error when tenant B reads tenant A's data")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestMultiTenancy_ListVersions_Filtered(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	// Create versions for two tenants on the same entity ID.
	ctxA := WithTenant(context.Background(), "tenant-a")
	ctxA = WithCustomMetadata(ctxA, "tenant_id", "tenant-a")
	p1, _ := a.Version(ctxA, &testUser{ID: "shared-id", Name: "A1"}, port.WithSync())
	p1.Wait(ctxA)

	ctxB := WithTenant(context.Background(), "tenant-b")
	ctxB = WithCustomMetadata(ctxB, "tenant_id", "tenant-b")
	p2, _ := a.Version(ctxB, &testUser{ID: "shared-id", Name: "B1"}, port.WithSync())
	p2.Wait(ctxB)

	scoped := NewTenantScopedReader(mw.reader)

	// Tenant A should only see their version.
	recsA, err := scoped.ListVersions(ctxA, "testuser", "shared-id")
	if err != nil {
		t.Fatalf("ListVersions tenant A: %v", err)
	}
	if len(recsA) != 1 {
		t.Errorf("tenant A expected 1 record, got %d", len(recsA))
	}

	// Tenant B should only see their version.
	recsB, err := scoped.ListVersions(ctxB, "testuser", "shared-id")
	if err != nil {
		t.Fatalf("ListVersions tenant B: %v", err)
	}
	if len(recsB) != 1 {
		t.Errorf("tenant B expected 1 record, got %d", len(recsB))
	}
}

func TestWithTenant_RoundTrip(t *testing.T) {
	ctx := WithTenant(context.Background(), "t1")
	if got := TenantFromCtx(ctx); got != "t1" {
		t.Errorf("TenantFromCtx = %q, want t1", got)
	}
}

func TestTenantFromCtx_Empty(t *testing.T) {
	if got := TenantFromCtx(context.Background()); got != "" {
		t.Errorf("TenantFromCtx on empty ctx = %q, want empty", got)
	}
}

func TestTenantScopedReader_NoTenant_PassThrough(t *testing.T) {
	// When no tenant is in context, all records pass through.
	inner := &memReader{
		records: []domain.VersionRecord{
			{EntityType: "test", EntityID: "e1", Version: 1, CreatedAt: time.Now()},
		},
	}
	scoped := NewTenantScopedReader(inner)

	rec, err := scoped.GetLatest(context.Background(), "test", "e1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.EntityID != "e1" {
		t.Errorf("expected e1, got %s", rec.EntityID)
	}
}
