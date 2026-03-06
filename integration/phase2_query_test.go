//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------------------------------------------------------------------------
// Phase 2 — Q-001 to Q-008: Query API tests
// ---------------------------------------------------------------------------

// Q-001: TestQuery_GetAtTime_ExactTimestamp retrieves version at exact CreatedAt.
func TestQuery_GetAtTime_ExactTimestamp(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	clk := newMockClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Clock = clk
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	clk.Advance(time.Second)
	rec1 := versionSync(t, ctx, a, user)

	clk.Advance(time.Second)
	user.Name = "v2"
	versionSync(t, ctx, a, user)

	// Query at v1's timestamp should return v1.
	got, err := a.GetAtTime(ctx, "userentity", user.ID, rec1.CreatedAt)
	if err != nil {
		t.Fatalf("GetAtTime: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("expected version 1, got %d", got.Version)
	}
}

// Q-002: TestQuery_GetAtTime_BetweenVersions queries between v2 and v3 → returns v2.
func TestQuery_GetAtTime_BetweenVersions(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	clk := newMockClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Clock = clk
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	clk.Advance(time.Second)
	versionSync(t, ctx, a, user) // v1 at t+1s

	clk.Advance(10 * time.Second)
	user.Name = "v2"
	versionSync(t, ctx, a, user) // v2 at t+11s

	clk.Advance(10 * time.Second)
	user.Name = "v3"
	versionSync(t, ctx, a, user) // v3 at t+21s

	// Query at t+15s (between v2 and v3) should return v2.
	queryTime := time.Date(2025, 1, 1, 0, 0, 16, 0, time.UTC)
	got, err := a.GetAtTime(ctx, "userentity", user.ID, queryTime)
	if err != nil {
		t.Fatalf("GetAtTime: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("expected version 2 (between v2 and v3), got %d", got.Version)
	}
}

// Q-003: TestQuery_GetAtTime_BeforeFirstVersion queries before v1 → ErrNotFound.
func TestQuery_GetAtTime_BeforeFirstVersion(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	clk := newMockClock(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Clock = clk
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	// Query well before v1's time.
	_, err := a.GetAtTime(ctx, "userentity", user.ID, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected ErrNotFound")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// Q-004: TestQuery_GetAtTime_AfterLatest queries far future → returns latest.
func TestQuery_GetAtTime_AfterLatest(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)
	user.Name = "v2"
	versionSync(t, ctx, a, user)

	got, err := a.GetAtTime(ctx, "userentity", user.ID, time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetAtTime: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("expected latest (v2), got %d", got.Version)
	}
}

// Q-005: TestQuery_ListVersions_DefaultPagination verifies default pagination.
func TestQuery_ListVersions_DefaultPagination(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "v0"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		user.Name = randString(3)
		versionSync(t, ctx, a, user)
	}

	records, err := a.ListVersions(ctx, "userentity", user.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(records) != 5 {
		t.Fatalf("expected 5 records, got %d", len(records))
	}
	// Default order is descending.
	if records[0].Version < records[len(records)-1].Version {
		t.Error("expected descending order by default")
	}
}

// Q-006: TestQuery_ListVersions_Ascending verifies ascending order.
func TestQuery_ListVersions_Ascending(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "v0"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		user.Name = randString(3)
		versionSync(t, ctx, a, user)
	}

	records, err := a.ListVersions(ctx, "userentity", user.ID, port.WithOrderAsc())
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	for i := 1; i < len(records); i++ {
		if records[i].Version <= records[i-1].Version {
			t.Errorf("expected ascending: v[%d]=%d <= v[%d]=%d",
				i, records[i].Version, i-1, records[i-1].Version)
		}
	}
}

// Q-007: TestQuery_ListVersions_LimitOffset verifies pagination with limit and offset.
func TestQuery_ListVersions_LimitOffset(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "v0"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		user.Name = randString(3)
		versionSync(t, ctx, a, user)
	}

	records, err := a.ListVersions(ctx, "userentity", user.ID,
		port.WithLimit(2), port.WithOffset(1), port.WithOrderAsc())
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records with limit=2, got %d", len(records))
	}
	// With offset=1, ascending, first record should be version 2.
	if records[0].Version != 2 {
		t.Errorf("expected first record version=2 (offset=1), got %d", records[0].Version)
	}
}

// Q-008: TestQuery_ListVersions_EmptyResult verifies empty result for non-existent entity.
func TestQuery_ListVersions_EmptyResult(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	// Register a dummy entity type so the auditor knows about it.
	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	records, err := a.ListVersions(ctx, "userentity", "nonexistent-id")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}
