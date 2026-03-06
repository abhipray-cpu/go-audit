//go:build chaos

package chaos

// ═══════════════════════════════════════════════════════════════════════════
// Chaos Tests — CHAOS-001 to CHAOS-008
//
// Infrastructure failure simulation against real Docker containers.
// These tests verify graceful degradation, data consistency under failure,
// tamper detection, and connection drop recovery.
//
// Run with: go test -tags chaos -race -timeout 20m -v ./...
// ═══════════════════════════════════════════════════════════════════════════

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	audit "github.com/abhipray-cpu/go-audit"
	s3store "github.com/abhipray-cpu/go-audit/adapter/coldstore/s3"
	mysqladapter "github.com/abhipray-cpu/go-audit/adapter/mysql"
	"github.com/abhipray-cpu/go-audit/adapter/postgres"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// CHAOS-001: Context cancellation mid-write. Verifies the system handles
// cancelled contexts gracefully without corrupting state.
func TestChaos_ContextCancelDuringWrite(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Write one good version to establish baseline.
	baseCtx := context.Background()
	eid := uniqueID("chaos")
	u := &UserEntity{ID: eid, Name: "baseline", Email: "c@c.com"}
	versionSync(t, baseCtx, a, u)

	// Attempt a write with an already-cancelled context.
	cancelCtx, cancel := context.WithCancel(baseCtx)
	cancel() // cancel immediately

	u.Name = "cancelled"
	pv, err := a.Version(cancelCtx, u, port.WithSync())
	if err == nil {
		_, err = pv.Wait(cancelCtx)
	}
	// We expect either an error or context cancellation — not a panic.
	t.Logf("CHAOS-001: cancelled write result: %v (no panic = pass)", err)

	// Original version must still be intact.
	rec, err := a.GetVersion(baseCtx, "userentity", eid, 1)
	if err != nil {
		t.Fatalf("original version lost: %v", err)
	}
	if rec.Version != 1 {
		t.Fatalf("version mismatch after chaos: expected 1, got %d", rec.Version)
	}
}

// CHAOS-002: Short deadline / timeout during read.
func TestChaos_TimeoutDuringRead(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	eid := uniqueID("timeout")
	u := &UserEntity{ID: eid, Name: "ok", Email: "t@t.com"}
	versionSync(t, ctx, a, u)

	// Read with a 1-nanosecond deadline — basically already expired.
	tinyCtx, cancel := context.WithTimeout(ctx, time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond) // ensure it's expired

	_, err := a.GetVersion(tinyCtx, "userentity", eid, 1)
	if err == nil {
		t.Log("CHAOS-002: read succeeded despite expired context (fast path hit)")
	} else {
		t.Logf("CHAOS-002: correctly got error on expired ctx: %v", err)
	}
}

// CHAOS-003: Shutdown during in-flight background work.
func TestChaos_ShutdownDuringBackgroundWork(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	// Create auditor without cleanup hook since we manually shutdown.
	w := postgres.NewWriter(conn)
	r := postgres.NewReader(conn)
	cfg := audit.Config{Writer: w, Reader: r, PoolWorkers: 1, PoolQueueSize: 50}
	a, err := audit.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	// Enqueue many background operations.
	for i := 0; i < 20; i++ {
		u := &UserEntity{ID: uniqueID("chaos"), Name: randString(5), Email: "s@s.com"}
		_, _ = a.Version(ctx, u) // fire and forget
	}

	// Immediately shutdown with tight deadline.
	shutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	err = a.Shutdown(shutCtx)
	t.Logf("CHAOS-003: shutdown during background work: %v (no panic = pass)", err)
}

// CHAOS-004: Double shutdown — calling Shutdown twice must not panic.
func TestChaos_DoubleShutdown(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	w := postgres.NewWriter(conn)
	r := postgres.NewReader(conn)
	a, err := audit.New(audit.Config{Writer: w, Reader: r})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	if err := a.Shutdown(ctx); err != nil {
		t.Fatalf("first shutdown: %v", err)
	}
	// Second shutdown — must not panic.
	if err := a.Shutdown(ctx); err != nil {
		t.Logf("CHAOS-004: second shutdown returned error (acceptable): %v", err)
	} else {
		t.Log("CHAOS-004: double shutdown passed without error")
	}
}

// CHAOS-005: MinIO unavailability during cold store write.
func TestChaos_MinIOUnavailable(t *testing.T) {
	skipIfNoMinIO(t)

	// Client pointing to a closed port to simulate MinIO being down.
	badClient := newMinioS3Client("http://localhost:19999", "x", "x")
	store := s3store.New(s3store.Config{
		Bucket: "chaos-test",
		Client: badClient,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := store.Put(ctx, "test/key/1", []byte(`{"test": true}`))
	if err == nil {
		t.Fatal("expected error writing to unavailable MinIO, got nil")
	}
	t.Logf("CHAOS-005: cold store correctly failed on unavailable MinIO: %v", err)
}

// CHAOS-006: Concurrent writers to the same entity — rapid sequential writes
// that stress version sequence numbering.
func TestChaos_DuplicateVersionRace(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	eid := uniqueID("dup")

	for i := 0; i < 50; i++ {
		u := &UserEntity{ID: eid, Name: fmt.Sprintf("v%d", i+1), Email: "d@d.com"}
		versionSync(t, ctx, a, u)
	}

	rec, err := a.GetLatest(ctx, "userentity", eid)
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if rec.Version != 50 {
		t.Fatalf("expected version 50, got %d", rec.Version)
	}
	t.Log("CHAOS-006: 50 rapid sequential writes maintained version integrity")
}

// CHAOS-007: Corrupt data in database — tamper with a stored record and verify
// VerifyIntegrity detects it.
func TestChaos_TamperDetection(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.EnableHashChain = true
	})
	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	eid := uniqueID("tamper")

	for i := 0; i < 3; i++ {
		u := &UserEntity{ID: eid, Name: fmt.Sprintf("v%d", i+1), Email: "t@t.com"}
		versionSync(t, ctx, a, u)
	}

	// Integrity should pass before tampering.
	if err := a.VerifyIntegrity(ctx, "userentity", eid); err != nil {
		t.Fatalf("integrity check failed before tampering: %v", err)
	}

	// Tamper: directly modify data of version 2.
	_, err := conn.Exec(ctx,
		`UPDATE audit_versions SET data = $1 WHERE entity_type = $2 AND entity_id = $3 AND version = $4`,
		[]byte(`{"ID":"TAMPERED","Name":"hacked"}`), "userentity", eid, 2)
	if err != nil {
		t.Fatalf("tamper exec: %v", err)
	}

	err = a.VerifyIntegrity(ctx, "userentity", eid)
	if err == nil {
		t.Fatal("BUG: VerifyIntegrity should detect tampered data, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation on tamper, got: %v", err)
	}
	t.Logf("CHAOS-007: tamper correctly detected: %v", err)
}

// CHAOS-008: Kill MySQL connection between operations by closing the db.
func TestChaos_MySQLConnectionDrop(t *testing.T) {
	db := connectMySQL(t)
	ctx := context.Background()
	cleanupMySQL(t, db)

	w := mysqladapter.NewWriter(db)
	r := mysqladapter.NewReader(db)
	a, err := audit.New(audit.Config{Writer: w, Reader: r})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := a.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	eid := uniqueID("mychao")
	u := &UserEntity{ID: eid, Name: "ok", Email: "m@m.com"}
	versionSync(t, ctx, a, u)

	// Close the DB connection, simulating a network failure.
	db.Close()

	// Attempt a write — should fail gracefully.
	u.Name = "after-close"
	pv, err := a.Version(ctx, u, port.WithSync())
	if err == nil {
		_, err = pv.Wait(ctx)
	}
	if err == nil {
		t.Fatal("expected error after connection drop, got nil")
	}
	t.Logf("CHAOS-008: MySQL connection drop handled gracefully: %v", err)
}
