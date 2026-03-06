//go:build integration

package integration

import (
	"context"
	"errors"
	"strings"
	"testing"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------------------------------------------------------------------------
// Phase 2 — G-001 to G-006: Entity Size Guardrails
// ---------------------------------------------------------------------------

// G-001: TestGuardrail_MaxEntitySize_Rejected verifies an entity exceeding
// MaxEntitySizeBytes is rejected with ErrEntityTooLarge.
func TestGuardrail_MaxEntitySize_Rejected(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.MaxEntitySizeBytes = 100 // very small limit
	})

	// Create a user with a large name that exceeds 100 bytes when serialized.
	user := &UserEntity{
		ID:   uniqueID("user"),
		Name: strings.Repeat("x", 200),
	}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	_, err := a.Version(ctx, user, port.WithSync())
	if err == nil {
		t.Fatal("expected error for oversized entity")
	}

	var tooLarge *domain.EntityTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected EntityTooLargeError, got: %v", err)
	}
}

// G-002: TestGuardrail_MaxEntitySize_Allowed verifies an entity within limits passes.
func TestGuardrail_MaxEntitySize_Allowed(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.MaxEntitySizeBytes = 10_000
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "small"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user) // should not error
}

// G-003: TestGuardrail_EmptyEntityID verifies empty entity ID is rejected.
func TestGuardrail_EmptyEntityID(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: "", Name: "no-id"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	_, err := a.Version(ctx, user, port.WithSync())
	if err == nil {
		t.Fatal("expected error for empty entity ID")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got: %v", err)
	}
}

// G-004: TestGuardrail_UnregisteredEntity verifies unregistered entity is rejected.
func TestGuardrail_UnregisteredEntity(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	// Do NOT register ProductEntity.
	product := &ProductEntity{ID: uniqueID("prod"), Name: "widget"}

	ctx := context.Background()
	_, err := a.Version(ctx, product, port.WithSync())
	if err == nil {
		t.Fatal("expected error for unregistered entity")
	}
	if !errors.Is(err, domain.ErrConfiguration) {
		t.Fatalf("expected ErrConfiguration, got: %v", err)
	}
}

// G-005: TestGuardrail_NilEntity verifies nil entity is rejected.
func TestGuardrail_NilEntity(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	ctx := context.Background()
	_, err := a.Version(ctx, nil, port.WithSync())
	if err == nil {
		t.Fatal("expected error for nil entity")
	}
}

// G-006: TestGuardrail_MaxEntitySize_Disabled verifies no limit when 0.
func TestGuardrail_MaxEntitySize_Disabled(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.MaxEntitySizeBytes = 0 // disabled
	})

	user := &UserEntity{
		ID:   uniqueID("user"),
		Name: strings.Repeat("x", 10000),
	}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user) // should not error
}
