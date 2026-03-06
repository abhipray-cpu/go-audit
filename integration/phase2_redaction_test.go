//go:build integration

package integration

import (
	"context"
	"testing"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/domain"
)

// ---------------------------------------------------------------------------
// Phase 2 — R-001 to R-006: Field Redaction (GDPR)
// ---------------------------------------------------------------------------

// R-001: TestRedact_TopLevelField verifies that RedactField("Email") replaces
// the Email value with "[REDACTED]" in all stored versions.
func TestRedact_TopLevelField(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "Alice", Email: "alice@example.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	count, err := a.RedactField(ctx, "userentity", user.ID, "Email")
	if err != nil {
		t.Fatalf("RedactField: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 version redacted, got %d", count)
	}

	// Read back and verify.
	rec, err := a.GetVersion(ctx, "userentity", user.ID, 1)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if !containsBytes(rec.Data, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] in data, got: %s", string(rec.Data))
	}
}

// R-002: TestRedact_NestedField verifies RedactField("Address.Street").
func TestRedact_NestedField(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{
		ID:      uniqueID("user"),
		Name:    "Bob",
		Email:   "bob@example.com",
		Address: Address{Street: "123 Main St", City: "NYC"},
	}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	count, err := a.RedactField(ctx, "userentity", user.ID, "Address.Street")
	if err != nil {
		t.Fatalf("RedactField: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1, got %d", count)
	}
}

// R-003: TestRedact_NonExistentField verifies 0 versions modified for fake field.
func TestRedact_NonExistentField(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "Carol", Email: "carol@test.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	count, err := a.RedactField(ctx, "userentity", user.ID, "FakeField")
	if err != nil {
		t.Fatalf("RedactField: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 versions redacted for non-existent field, got %d", count)
	}
}

// R-004: TestRedact_AlreadyRedacted verifies redacting twice is idempotent.
func TestRedact_AlreadyRedacted(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "Dave", Email: "dave@test.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	_, err := a.RedactField(ctx, "userentity", user.ID, "Email")
	if err != nil {
		t.Fatalf("first RedactField: %v", err)
	}

	count, err := a.RedactField(ctx, "userentity", user.ID, "Email")
	if err != nil {
		t.Fatalf("second RedactField: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 on idempotent redact, got %d", count)
	}
}

// R-005: TestRedact_PreservesOtherFields verifies unrelated fields are unchanged.
func TestRedact_PreservesOtherFields(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "Eve", Email: "eve@test.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	_, err := a.RedactField(ctx, "userentity", user.ID, "Email")
	if err != nil {
		t.Fatalf("RedactField: %v", err)
	}

	rec, err := a.GetVersion(ctx, "userentity", user.ID, 1)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}

	// Name should still be present.
	if !containsBytes(rec.Data, "Eve") {
		t.Fatalf("expected Name to be preserved, got: %s", string(rec.Data))
	}
}

// R-006: TestRedact_MultipleVersions verifies all versions get redacted.
func TestRedact_MultipleVersions(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Strategy = domain.StrategyFull
	})
	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "frank@test.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		user.Name = randString(5)
		versionSync(t, ctx, a, user)
	}

	count, err := a.RedactField(ctx, "userentity", user.ID, "Email")
	if err != nil {
		t.Fatalf("RedactField: %v", err)
	}
	if count != 5 {
		t.Fatalf("expected 5 versions redacted, got %d", count)
	}
}

// containsBytes checks if data contains the given substring.
func containsBytes(data []byte, sub string) bool {
	return len(data) > 0 && len(sub) > 0 && bytesContains(data, []byte(sub))
}

func bytesContains(haystack, needle []byte) bool {
	return len(haystack) >= len(needle) && bytesIndex(haystack, needle) >= 0
}

func bytesIndex(s, sep []byte) int {
	for i := 0; i <= len(s)-len(sep); i++ {
		match := true
		for j := range sep {
			if s[i+j] != sep[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
