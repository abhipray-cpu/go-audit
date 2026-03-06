//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/adapter/hash"
	"github.com/abhipray-cpu/go-audit/domain"
)

// ---------------------------------------------------------------------------
// Phase 2 — I-001 to I-008: Hash Chain & Integrity Verification
// ---------------------------------------------------------------------------

// I-001: TestIntegrity_ChainCreated verifies that EnableHashChain=true produces
// Hash and PreviousHash on every version record.
func TestIntegrity_ChainCreated(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.EnableHashChain = true
		c.Hasher = hash.NewSHA256()
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	rec := versionSync(t, ctx, a, user)

	if rec.Hash == "" {
		t.Fatal("expected non-empty Hash on version 1")
	}
	// Version 1 should have empty PreviousHash.
	if rec.PreviousHash != "" {
		t.Fatalf("expected empty PreviousHash on v1, got %q", rec.PreviousHash)
	}
}

// I-002: TestIntegrity_V1_NoPreviousHash confirms PreviousHash="" on version 1.
func TestIntegrity_V1_NoPreviousHash(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.EnableHashChain = true
		c.Hasher = hash.NewSHA256()
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	rec := versionSync(t, ctx, a, user)

	if rec.PreviousHash != "" {
		t.Fatalf("v1 PreviousHash should be empty, got %q", rec.PreviousHash)
	}
}

// I-003: TestIntegrity_V2_ChainsToPrevious confirms v2's PreviousHash == v1's Hash.
func TestIntegrity_V2_ChainsToPrevious(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.EnableHashChain = true
		c.Hasher = hash.NewSHA256()
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	rec1 := versionSync(t, ctx, a, user)

	user.Name = "v2"
	rec2 := versionSync(t, ctx, a, user)

	if rec2.PreviousHash != rec1.Hash {
		t.Fatalf("v2 PreviousHash=%q, want v1 Hash=%q", rec2.PreviousHash, rec1.Hash)
	}
}

// I-004: TestIntegrity_Verify_ValidChain confirms VerifyIntegrity returns nil
// on an untampered chain.
func TestIntegrity_Verify_ValidChain(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.EnableHashChain = true
		c.Hasher = hash.NewSHA256()
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)
	user.Name = "v2"
	versionSync(t, ctx, a, user)
	user.Name = "v3"
	versionSync(t, ctx, a, user)

	if err := a.VerifyIntegrity(ctx, "userentity", user.ID); err != nil {
		t.Fatalf("VerifyIntegrity should pass: %v", err)
	}
}

// I-005: TestIntegrity_Verify_TamperedData modifies stored data directly
// and confirms VerifyIntegrity detects tampering.
func TestIntegrity_Verify_TamperedData(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.EnableHashChain = true
		c.Hasher = hash.NewSHA256()
	})

	eid := uniqueID("user")
	user := &UserEntity{ID: eid, Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)
	user.Name = "v2"
	versionSync(t, ctx, a, user)

	// Tamper: update data directly in the database.
	_, err := conn.Exec(ctx,
		`UPDATE audit_versions SET data = '{"ID":"tampered"}' WHERE entity_type = 'userentity' AND entity_id = $1 AND version = 1`, eid)
	if err != nil {
		t.Fatalf("tamper exec: %v", err)
	}

	if err := a.VerifyIntegrity(ctx, "userentity", eid); err == nil {
		t.Fatal("VerifyIntegrity should detect tampered data")
	}
}

// I-006: TestIntegrity_Verify_TamperedHash modifies the stored hash directly
// and confirms VerifyIntegrity detects tampering.
func TestIntegrity_Verify_TamperedHash(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.EnableHashChain = true
		c.Hasher = hash.NewSHA256()
	})

	eid := uniqueID("user")
	user := &UserEntity{ID: eid, Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	// Tamper: update hash directly.
	_, err := conn.Exec(ctx,
		`UPDATE audit_versions SET hash = 'deadbeef' WHERE entity_type = 'userentity' AND entity_id = $1 AND version = 1`, eid)
	if err != nil {
		t.Fatalf("tamper exec: %v", err)
	}

	user.Name = "v2"
	versionSync(t, ctx, a, user)

	if err := a.VerifyIntegrity(ctx, "userentity", eid); err == nil {
		t.Fatal("VerifyIntegrity should detect tampered hash")
	}
}

// I-007: TestIntegrity_Disabled_NoHash confirms Hash and PreviousHash are empty
// when EnableHashChain is false.
func TestIntegrity_Disabled_NoHash(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn) // no hash chain

	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	rec := versionSync(t, ctx, a, user)

	if rec.Hash != "" {
		t.Fatalf("expected empty Hash when hash chain disabled, got %q", rec.Hash)
	}
	if rec.PreviousHash != "" {
		t.Fatalf("expected empty PreviousHash when hash chain disabled, got %q", rec.PreviousHash)
	}
}

// I-008: TestIntegrity_Verify_NotEnabled confirms VerifyIntegrity returns
// ErrConfiguration when hash chain is disabled.
func TestIntegrity_Verify_NotEnabled(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn) // no hash chain

	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	err := a.VerifyIntegrity(ctx, "userentity", user.ID)
	if err == nil {
		t.Fatal("expected error when hash chain disabled")
	}
	if !errors.Is(err, domain.ErrConfiguration) {
		t.Fatalf("expected ErrConfiguration, got: %v", err)
	}
}
