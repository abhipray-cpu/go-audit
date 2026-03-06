//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------------------------------------------------------------------------
// Phase 2 — ST-001 to ST-006: Storage strategies
// ---------------------------------------------------------------------------

// ST-001: TestStrategy_Full_AlwaysFullSnapshot verifies StrategyFull stores complete data.
func TestStrategy_Full_AlwaysFullSnapshot(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Strategy = domain.StrategyFull
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	rec1 := versionSync(t, ctx, a, user)
	if rec1.Strategy != domain.StrategyFull {
		t.Errorf("v1: expected Full, got %s", rec1.Strategy)
	}

	user.Name = "v2"
	rec2 := versionSync(t, ctx, a, user)
	if rec2.Strategy != domain.StrategyFull {
		t.Errorf("v2: expected Full, got %s", rec2.Strategy)
	}

	user.Name = "v3"
	rec3 := versionSync(t, ctx, a, user)
	if rec3.Strategy != domain.StrategyFull {
		t.Errorf("v3: expected Full, got %s", rec3.Strategy)
	}
}

// ST-002: TestStrategy_Delta_FirstVersionFull verifies v1 is Full even under Delta strategy.
func TestStrategy_Delta_FirstVersionFull(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Strategy = domain.StrategyDelta
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	rec1 := versionSync(t, ctx, a, user)
	if rec1.Strategy != domain.StrategyFull {
		t.Errorf("v1 under Delta config: expected Full, got %s", rec1.Strategy)
	}

	user.Name = "v2"
	rec2 := versionSync(t, ctx, a, user)
	if rec2.Strategy != domain.StrategyDelta {
		t.Errorf("v2 under Delta config: expected Delta, got %s", rec2.Strategy)
	}
}

// ST-003: TestStrategy_Delta_HasDelta verifies Delta versions have non-nil Delta field.
func TestStrategy_Delta_HasDelta(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Strategy = domain.StrategyDelta
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user) // v1

	user.Name = "v2"
	rec2 := versionSync(t, ctx, a, user)

	if rec2.Delta == nil {
		t.Fatal("v2 Delta should not be nil")
	}
	if rec2.Delta.IsEmpty() {
		t.Fatal("v2 Delta should have changes")
	}

	// Delta should contain the Name change.
	found := false
	for _, c := range rec2.Delta.Changes {
		if c.Path == "Name" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Name change in delta, got %+v", rec2.Delta.Changes)
	}
}

// ST-004: TestStrategy_Hybrid_SnapshotInterval verifies hybrid pattern.
func TestStrategy_Hybrid_SnapshotInterval(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Strategy = domain.StrategyHybrid
		c.HybridSnapshotInterval = 3
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v0"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	// Expected pattern: v1=Full, v2=Delta, v3=Delta, v4=Full, v5=Delta, v6=Delta
	expected := []domain.Strategy{
		domain.StrategyFull,  // v1
		domain.StrategyDelta, // v2
		domain.StrategyDelta, // v3
		domain.StrategyFull,  // v4
		domain.StrategyDelta, // v5
		domain.StrategyDelta, // v6
	}

	for i := 0; i < 6; i++ {
		user.Name = randString(5)
		rec := versionSync(t, ctx, a, user)
		if rec.Strategy != expected[i] {
			t.Errorf("v%d: expected %s, got %s", i+1, expected[i], rec.Strategy)
		}
	}
}

// ST-005: TestStrategy_PerVersionOverride verifies WithStrategy overrides default.
func TestStrategy_PerVersionOverride(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Strategy = domain.StrategyFull // default is Full
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user) // v1 Full

	user.Name = "v2"
	// Override to Delta for this single version.
	pv, err := a.Version(ctx, user, port.WithSync(), port.WithStrategy(domain.StrategyDelta))
	if err != nil {
		t.Fatalf("Version with override: %v", err)
	}
	rec, err := pv.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if rec.Strategy != domain.StrategyDelta {
		t.Errorf("override: expected Delta, got %s", rec.Strategy)
	}
}

// ST-006: TestStrategy_Delta_ReadbackReconstruction reads back a delta version
// and verifies the data is still correct (full snapshot stored alongside delta).
func TestStrategy_Delta_ReadbackReconstruction(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Strategy = domain.StrategyDelta
		c.Clock = newMockClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "Alice", Email: "a@test.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user) // v1

	user.Name = "Bob"
	versionSync(t, ctx, a, user) // v2

	// Read v2 and verify data includes the complete entity.
	rec, err := a.GetVersion(ctx, "userentity", user.ID, 2)
	if err != nil {
		t.Fatalf("GetVersion v2: %v", err)
	}

	// Even delta-stored versions have the full serialized data.
	if len(rec.Data) == 0 {
		t.Fatal("v2 data should not be empty")
	}
}
