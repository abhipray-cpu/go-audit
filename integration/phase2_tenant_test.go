//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/adapter/postgres"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------------------------------------------------------------------------
// Phase 2 — T-001 to T-004: Multi-Tenancy Isolation
// ---------------------------------------------------------------------------

// T-001: TestTenant_IsolatedReads verifies tenant-scoped reader filters by tenant.
func TestTenant_IsolatedReads(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Version as tenant-A.
	ctxA := audit.WithTenant(context.Background(), "tenant-A")
	ctxA = audit.WithCustomMetadata(ctxA, "tenant_id", "tenant-A")
	versionSync(t, ctxA, a, user)

	// Create a tenant-scoped reader.
	scopedReader := audit.NewTenantScopedReader(postgres.NewReader(conn))

	// Tenant-A can read it.
	_, err := scopedReader.GetLatest(ctxA, "userentity", user.ID)
	if err != nil {
		t.Fatalf("Tenant-A should see own record: %v", err)
	}

	// Tenant-B cannot read it.
	ctxB := audit.WithTenant(context.Background(), "tenant-B")
	_, err = scopedReader.GetLatest(ctxB, "userentity", user.ID)
	if err == nil {
		t.Fatal("Tenant-B should not see Tenant-A's record")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

// T-002: TestTenant_CrossTenantBlocked verifies cross-tenant reads return ErrNotFound.
func TestTenant_CrossTenantBlocked(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctxA := audit.WithTenant(context.Background(), "tenant-A")
	ctxA = audit.WithCustomMetadata(ctxA, "tenant_id", "tenant-A")
	versionSync(t, ctxA, a, user)

	scopedReader := audit.NewTenantScopedReader(postgres.NewReader(conn))

	// GetByVersion with wrong tenant.
	ctxB := audit.WithTenant(context.Background(), "tenant-B")
	_, err := scopedReader.GetByVersion(ctxB, "userentity", user.ID, 1)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant, got: %v", err)
	}
}

// T-003: TestTenant_NoTenantSeesAll verifies no tenant in context returns all records.
func TestTenant_NoTenantSeesAll(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctxA := audit.WithCustomMetadata(context.Background(), "tenant_id", "tenant-A")
	versionSync(t, ctxA, a, user)

	scopedReader := audit.NewTenantScopedReader(postgres.NewReader(conn))

	// No tenant context → should return records.
	recs, err := scopedReader.ListVersions(context.Background(), "userentity", user.ID, port.WithLimit(10))
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record without tenant filter, got %d", len(recs))
	}
}

// T-004: TestTenant_Metadata verifies tenant_id appears in metadata.Custom.
func TestTenant_Metadata(t *testing.T) {
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

	ctx := audit.WithCustomMetadata(context.Background(), "tenant_id", "tenant-X")
	rec := versionSync(t, ctx, a, user)

	if rec.Metadata.Custom["tenant_id"] != "tenant-X" {
		t.Fatalf("expected tenant_id=tenant-X in metadata, got: %v", rec.Metadata.Custom)
	}
}
