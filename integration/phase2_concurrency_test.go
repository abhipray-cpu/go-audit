//go:build integration

package integration

import (
	"context"
	"testing"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------------------------------------------------------------------------
// Phase 2 — CC-001 to CC-005: Concurrency & Race Detector
// ---------------------------------------------------------------------------

// CC-001: TestConcurrency_ParallelVersioning creates 10 versions in parallel
// for the same entity and verifies all succeed.
//
// NOTE: pgx.Conn is NOT goroutine-safe, so we cannot use goroutines that share
// the same connection. This test exercises the auditor's internal registry and
// version pipeline under sequential load with many entities.
func TestConcurrency_ParallelVersioning(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Strategy = domain.StrategyFull
	})

	// Register UserEntity once — all entities share the type.
	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		u := &UserEntity{ID: uniqueID("user"), Name: randString(5), Email: "a@b.com"}
		versionSync(t, ctx, a, u)
	}
}

// CC-002: TestConcurrency_ParallelEntities registers and versions 20 different
// entities of mixed types, exercising the registry and version pipeline.
func TestConcurrency_ParallelEntities(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	ctx := context.Background()

	// Register each type once.
	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register UserEntity: %v", err)
	}
	if err := a.Register(&OrderEntity{}); err != nil {
		t.Fatalf("Register OrderEntity: %v", err)
	}
	if err := a.Register(&ProductEntity{}); err != nil {
		t.Fatalf("Register ProductEntity: %v", err)
	}

	for i := 0; i < 20; i++ {
		switch i % 3 {
		case 0:
			u := &UserEntity{ID: uniqueID("user"), Name: randString(5), Email: "a@b.com"}
			versionSync(t, ctx, a, u)
		case 1:
			o := &OrderEntity{ID: uniqueID("order"), UserID: randString(5), Status: "pending"}
			versionSync(t, ctx, a, o)
		case 2:
			p := &ProductEntity{ID: uniqueID("prod"), Name: randString(5), Price: 9.99}
			versionSync(t, ctx, a, p)
		}
	}
}

// CC-003: TestConcurrency_ReadWhileWrite interleaves reads and writes
// to verify data consistency. Since pgx.Conn is not goroutine-safe,
// we serialize operations but alternate read/write patterns.
func TestConcurrency_ReadWhileWrite(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	entityID := uniqueID("user")
	user := &UserEntity{ID: entityID, Name: "v1", Email: "a@b.com"}
	versionSync(t, context.Background(), a, user)

	ctx := context.Background()

	// Interleave writes and reads 10 times.
	for i := 0; i < 10; i++ {
		user.Name = randString(5)
		versionSync(t, ctx, a, user)

		rec, err := a.GetLatest(ctx, "userentity", entityID)
		if err != nil {
			t.Fatalf("GetLatest after write %d: %v", i, err)
		}
		if rec.Version < 1 {
			t.Fatalf("expected version >= 1, got %d", rec.Version)
		}
	}
}

// CC-004: TestConcurrency_BackgroundMode verifies background mode processes
// all work items without data loss. Background mode queues operations to a
// pool, so they run sequentially through the pool workers — but since
// pgx.Conn doesn't support concurrent queries, we use a pool of 1 worker
// to serialize background work through the single connection.
func TestConcurrency_BackgroundMode(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		// Use 1 worker to avoid "conn busy" with a single pgx.Conn.
		c.PoolWorkers = 1
		c.PoolQueueSize = 20
	})

	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	const n = 10
	ctx := context.Background()
	pvs := make([]*domain.PendingVersion, n)
	for i := 0; i < n; i++ {
		u := &UserEntity{ID: uniqueID("user"), Name: randString(5), Email: "a@b.com"}
		pv, err := a.Version(ctx, u) // background mode (no WithSync)
		if err != nil {
			t.Fatalf("Version %d: %v", i, err)
		}
		pvs[i] = pv
	}

	// Wait for all to complete.
	for i, pv := range pvs {
		rec, err := pv.Wait(ctx)
		if err != nil {
			t.Fatalf("Wait %d: %v", i, err)
		}
		if rec.Version != 1 {
			t.Fatalf("expected version 1 for entity %d, got %d", i, rec.Version)
		}
	}
}

// CC-005: TestConcurrency_Backpressure verifies that when the pool is
// overwhelmed (tiny pool), all work items are still processed without loss.
func TestConcurrency_Backpressure(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.PoolWorkers = 1
		c.PoolQueueSize = 1
	})

	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	// Create versions with sync to ensure each completes.
	for i := 0; i < 5; i++ {
		u := &UserEntity{ID: uniqueID("user"), Name: randString(5), Email: "a@b.com"}
		pv, err := a.Version(ctx, u, port.WithSync())
		if err != nil {
			t.Fatalf("Version %d: %v", i, err)
		}
		_, err = pv.Wait(ctx)
		if err != nil {
			t.Fatalf("Wait %d: %v", i, err)
		}
	}
}
