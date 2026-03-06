//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------------------------------------------------------------------------
// Phase 2 — C-001 to C-010: Compare, FindChanges, FindByActor, GetBulkAtTime
// ---------------------------------------------------------------------------

// C-001: TestCompare_V1toV2 compares v1→v2 and expects a non-empty delta.
func TestCompare_V1toV2(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "Alice", Email: "a@test.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)
	user.Name = "Bob"
	versionSync(t, ctx, a, user)

	delta, err := a.Compare(ctx, "userentity", user.ID, 1, 2)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if delta.IsEmpty() {
		t.Fatal("expected non-empty delta between v1 and v2")
	}
	found := false
	for _, c := range delta.Changes {
		if c.Path == "Name" {
			found = true
		}
	}
	if !found {
		t.Error("expected Name change in delta")
	}
}

// C-002: TestCompare_V1toV5 compares across multiple versions.
func TestCompare_V1toV5(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "v1", Email: "v1@test.com", Age: 20}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user) // v1
	for i := 2; i <= 5; i++ {
		user.Name = randString(5)
		user.Age = 20 + i
		versionSync(t, ctx, a, user)
	}

	delta, err := a.Compare(ctx, "userentity", user.ID, 1, 5)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if delta.IsEmpty() {
		t.Fatal("expected non-empty delta between v1 and v5")
	}
}

// C-003: TestCompare_SameVersion compares same version → empty delta.
func TestCompare_SameVersion(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "Alice"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	delta, err := a.Compare(ctx, "userentity", user.ID, 1, 1)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if !delta.IsEmpty() {
		t.Errorf("expected empty delta for same version, got %d changes", len(delta.Changes))
	}
}

// C-004: TestCompare_NonExistent compares with missing version → ErrNotFound.
func TestCompare_NonExistent(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "Alice"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	_, err := a.Compare(ctx, "userentity", user.ID, 1, 999)
	if err == nil {
		t.Fatal("expected error for non-existent version")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// C-005: TestFindChanges_SpecificField finds changes to a specific field.
func TestFindChanges_SpecificField(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "Alice", Email: "a@test.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user) // v1
	user.Name = "Bob"
	versionSync(t, ctx, a, user) // v2
	user.Name = "Charlie"
	user.Email = "c@test.com"
	versionSync(t, ctx, a, user) // v3

	changes, err := a.FindChanges(ctx, "userentity", user.ID, "Name")
	if err != nil {
		t.Fatalf("FindChanges: %v", err)
	}
	// Name changed v1→v2 and v2→v3 = 2 changes.
	if len(changes) != 2 {
		t.Errorf("expected 2 Name changes, got %d", len(changes))
	}
}

// C-006: TestFindChanges_NestedField finds changes to a nested field.
func TestFindChanges_NestedField(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "Alice", Address: Address{City: "NYC"}}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)
	user.Address.City = "LA"
	versionSync(t, ctx, a, user)

	changes, err := a.FindChanges(ctx, "userentity", user.ID, "Address.City")
	if err != nil {
		t.Fatalf("FindChanges: %v", err)
	}
	if len(changes) != 1 {
		t.Errorf("expected 1 Address.City change, got %d", len(changes))
	}
}

// C-007: TestFindChanges_NoChanges finds no changes for unmodified field.
func TestFindChanges_NoChanges(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "Alice", Email: "fixed@test.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)
	user.Name = "Bob" // only Name changes, not Email
	versionSync(t, ctx, a, user)

	changes, err := a.FindChanges(ctx, "userentity", user.ID, "Email")
	if err != nil {
		t.Fatalf("FindChanges: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("expected 0 Email changes, got %d", len(changes))
	}
}

// C-008: TestFindByActor_FiltersByActor filters by actor ID.
func TestFindByActor_FiltersByActor(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	ctx1 := audit.WithActor(ctx, "actor-A", domain.ActorHuman)
	pv1, err := a.Version(ctx1, user, port.WithSync())
	if err != nil {
		t.Fatalf("Version 1: %v", err)
	}
	if _, err := pv1.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	user.Name = "v2"
	ctx2 := audit.WithActor(ctx, "actor-B", domain.ActorSystem)
	pv2, err := a.Version(ctx2, user, port.WithSync())
	if err != nil {
		t.Fatalf("Version 2: %v", err)
	}
	if _, err := pv2.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	records, err := a.FindByActor(ctx, "userentity", user.ID, "actor-A")
	if err != nil {
		t.Fatalf("FindByActor: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 record from actor-A, got %d", len(records))
	}
}

// C-009: TestFindByActor_NoMatch finds no records for non-existent actor.
func TestFindByActor_NoMatch(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	records, err := a.FindByActor(ctx, "userentity", user.ID, "nonexistent-actor")
	if err != nil {
		t.Fatalf("FindByActor: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

// C-010: TestGetBulkAtTime retrieves versions for multiple entities at a past time.
func TestGetBulkAtTime(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	ctx := context.Background()
	ids := make([]string, 5)
	for i := 0; i < 5; i++ {
		u := &UserEntity{ID: uniqueID("user"), Name: randString(5)}
		if i == 0 {
			if err := a.Register(u); err != nil {
				t.Fatalf("Register: %v", err)
			}
		}
		ids[i] = u.ID
		versionSync(t, ctx, a, u)
	}

	// Query all 5 at future time.
	records, err := a.GetBulkAtTime(ctx, "userentity", ids, futureTime())
	if err != nil {
		t.Fatalf("GetBulkAtTime: %v", err)
	}
	if len(records) != 5 {
		t.Errorf("expected 5 records, got %d", len(records))
	}
}
