//go:build loadtest

package load

// ═══════════════════════════════════════════════════════════════════════════
// Load Tests — LOAD-001 to LOAD-007
//
// High-throughput and saturation tests against real Docker infrastructure.
// These tests exercise sustained writes, burst traffic, backpressure,
// large payloads, size guardrails, MySQL throughput, and read-after-write
// consistency.
//
// Run with: go test -tags loadtest -race -timeout 45m -v ./...
// ═══════════════════════════════════════════════════════════════════════════

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	audit "github.com/abhipray-cpu/go-audit"
	mysqladapter "github.com/abhipray-cpu/go-audit/adapter/mysql"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// LOAD-001: Sustained sequential write throughput. Writes 500 versions of the
// same entity, verifying monotonic version numbers and data integrity.
func TestLoad_SequentialWriteThroughput(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	const N = 500
	ctx := context.Background()
	entityID := uniqueID("loaduser")

	start := time.Now()
	for i := 0; i < N; i++ {
		u := &UserEntity{ID: entityID, Name: fmt.Sprintf("v%d", i+1), Email: fmt.Sprintf("u%d@test.com", i)}
		versionSync(t, ctx, a, u)
	}
	dur := time.Since(start)
	t.Logf("LOAD-001: %d sequential writes in %v (%.0f writes/sec)", N, dur, float64(N)/dur.Seconds())

	// Verify monotonic sequence.
	records, err := a.ListVersions(ctx, "userentity", entityID, port.WithLimit(N), port.WithOrderAsc())
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(records) != N {
		t.Fatalf("expected %d records, got %d", N, len(records))
	}
	for i, rec := range records {
		if rec.Version != int64(i+1) {
			t.Errorf("record %d: expected version %d, got %d", i, i+1, rec.Version)
		}
	}
}

// LOAD-002: Multiple distinct entities under high load.
// 100 different entities × 10 versions each = 1000 records.
func TestLoad_MultiEntityBurst(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	const entities = 100
	const versionsPerEntity = 10

	ctx := context.Background()
	start := time.Now()
	for e := 0; e < entities; e++ {
		eid := uniqueID("loadburst")
		for v := 0; v < versionsPerEntity; v++ {
			u := &UserEntity{ID: eid, Name: fmt.Sprintf("v%d", v+1), Email: "e@x.com"}
			versionSync(t, ctx, a, u)
		}
	}
	dur := time.Since(start)
	total := entities * versionsPerEntity
	t.Logf("LOAD-002: %d entities × %d versions = %d writes in %v (%.0f w/s)",
		entities, versionsPerEntity, total, dur, float64(total)/dur.Seconds())
}

// LOAD-003: Pool backpressure with tiny queue.
// Overflows the background pool to trigger on-backpressure synchronous fallback.
func TestLoad_BackpressureOverflow(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.PoolWorkers = 1
		c.PoolQueueSize = 2 // intentionally tiny
	})
	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	const N = 50
	ctx := context.Background()

	// Submit many background versions at once.
	pvs := make([]*domain.PendingVersion, N)
	for i := 0; i < N; i++ {
		u := &UserEntity{ID: uniqueID("bp"), Name: fmt.Sprintf("bp-%d", i), Email: "bp@test.com"}
		pv, err := a.Version(ctx, u) // background mode
		if err != nil {
			t.Fatalf("Version %d: %v", i, err)
		}
		pvs[i] = pv
	}

	// All must complete (backpressure handler runs synchronously).
	for i, pv := range pvs {
		if _, err := pv.Wait(ctx); err != nil {
			t.Fatalf("Wait %d: %v", i, err)
		}
	}
	t.Logf("LOAD-003: %d items survived backpressure", N)
}

// LOAD-004: Large entity payload — 1 MB JSON blob.
func TestLoad_LargePayload(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	bigSettings := make(map[string]string, 5000)
	for i := 0; i < 5000; i++ {
		bigSettings[fmt.Sprintf("key-%05d", i)] = strings.Repeat("X", 200)
	}
	u := &UserEntity{
		ID:       uniqueID("large"),
		Name:     "BigUser",
		Email:    "big@test.com",
		Settings: bigSettings,
	}
	rec := versionSync(t, ctx, a, u)
	t.Logf("LOAD-004: stored %d byte payload successfully", len(rec.Data))

	// Verify round-trip.
	got, err := a.GetVersion(ctx, "userentity", u.ID, 1)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if !bytes.Equal(got.Data, rec.Data) {
		t.Fatal("round-trip data mismatch on large payload")
	}
}

// LOAD-005: Entity size guardrail under load — ensure MaxEntitySizeBytes rejects.
func TestLoad_EntitySizeGuardrail(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.MaxEntitySizeBytes = 1024 // 1 KB limit
	})
	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	u := &UserEntity{
		ID:    uniqueID("big"),
		Name:  strings.Repeat("A", 2000), // > 1KB when serialized
		Email: "x@x.com",
	}
	pv, err := a.Version(ctx, u, port.WithSync())
	if err == nil {
		_, waitErr := pv.Wait(ctx)
		if waitErr == nil {
			t.Fatal("expected ErrEntityTooLarge, got nil")
		}
		err = waitErr
	}
	if !errors.Is(err, domain.ErrEntityTooLarge) {
		t.Fatalf("expected ErrEntityTooLarge, got %v", err)
	}
	t.Log("LOAD-005: size guardrail correctly rejected oversized entity")
}

// LOAD-006: MySQL sustained write throughput.
func TestLoad_MySQLThroughput(t *testing.T) {
	db := connectMySQL(t)
	defer db.Close()

	ctx := context.Background()
	cleanupMySQL(t, db)

	w := mysqladapter.NewWriter(db)
	r := mysqladapter.NewReader(db)
	a, err := audit.New(audit.Config{Writer: w, Reader: r})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(ctx)

	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	const N = 200
	eid := uniqueID("myload")
	start := time.Now()
	for i := 0; i < N; i++ {
		u := &UserEntity{ID: eid, Name: fmt.Sprintf("v%d", i+1), Email: "m@y.com"}
		versionSync(t, ctx, a, u)
	}
	dur := time.Since(start)
	t.Logf("LOAD-006: MySQL %d writes in %v (%.0f w/s)", N, dur, float64(N)/dur.Seconds())
}

// LOAD-007: Rapid read after write — query immediately after each write to
// check data availability under high throughput.
func TestLoad_ReadAfterWrite(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	const N = 200
	ctx := context.Background()
	eid := uniqueID("raw")

	for i := 0; i < N; i++ {
		u := &UserEntity{ID: eid, Name: fmt.Sprintf("v%d", i+1), Email: "rw@test.com"}
		versionSync(t, ctx, a, u)

		// Immediately read back.
		rec, err := a.GetVersion(ctx, "userentity", eid, int64(i+1))
		if err != nil {
			t.Fatalf("read-after-write v%d: %v", i+1, err)
		}
		if rec.Version != int64(i+1) {
			t.Fatalf("expected version %d, got %d", i+1, rec.Version)
		}
	}
	t.Logf("LOAD-007: %d read-after-write cycles passed", N)
}
