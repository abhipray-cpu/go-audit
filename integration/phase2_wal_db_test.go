//go:build integration

package integration

import (
	"context"
	"testing"

	waldb "github.com/abhipray-cpu/go-audit/adapter/wal/db"
	"github.com/abhipray-cpu/go-audit/domain/port"
	"github.com/jackc/pgx/v5"
)

// ---------------------------------------------------------------------------
// Phase 2 — WD-001 to WD-004: DB WAL (Postgres-backed)
// ---------------------------------------------------------------------------

// pgxWALDB wraps *pgx.Conn to satisfy waldb.WALDB interface.
type pgxWALDB struct {
	conn *pgx.Conn
}

func (p *pgxWALDB) ExecContext(ctx context.Context, query string, args ...any) (waldb.Result, error) {
	ct, err := p.conn.Exec(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &pgxWALResult{affected: ct.RowsAffected()}, nil
}

func (p *pgxWALDB) QueryContext(ctx context.Context, query string, args ...any) (waldb.Rows, error) {
	rows, err := p.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &pgxWALRows{rows}, nil
}

type pgxWALResult struct {
	affected int64
}

func (r *pgxWALResult) RowsAffected() (int64, error) {
	return r.affected, nil
}

type pgxWALRows struct {
	rows pgx.Rows
}

func (r *pgxWALRows) Next() bool             { return r.rows.Next() }
func (r *pgxWALRows) Scan(dest ...any) error { return r.rows.Scan(dest...) }
func (r *pgxWALRows) Close() error           { r.rows.Close(); return nil }
func (r *pgxWALRows) Err() error             { return r.rows.Err() }

// WD-001: TestWAL_DB_EnsureTable verifies the WAL table is created.
func TestWAL_DB_EnsureTable(t *testing.T) {
	conn := connectPostgres(t)
	ctx := context.Background()
	_, _ = conn.Exec(ctx, "DROP TABLE IF EXISTS audit_wal_entries")

	w := waldb.New(&pgxWALDB{conn: conn})
	if err := w.EnsureTable(ctx); err != nil {
		t.Fatalf("EnsureTable: %v", err)
	}

	// Verify table exists by querying it.
	rows, err := conn.Query(ctx, "SELECT id FROM audit_wal_entries LIMIT 1")
	if err != nil {
		t.Fatalf("table should exist: %v", err)
	}
	rows.Close()
}

// WD-002: TestWAL_DB_AppendAndReplay appends entries and replays all.
func TestWAL_DB_AppendAndReplay(t *testing.T) {
	conn := connectPostgres(t)
	ctx := context.Background()
	_, _ = conn.Exec(ctx, "DROP TABLE IF EXISTS audit_wal_entries")

	w := waldb.New(&pgxWALDB{conn: conn})
	if err := w.EnsureTable(ctx); err != nil {
		t.Fatalf("EnsureTable: %v", err)
	}

	for i := 0; i < 3; i++ {
		entry := port.WALEntry{ID: uniqueID("dbwal")}
		if err := w.Append(ctx, entry); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	entries, err := w.Replay(ctx)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
}

// WD-003: TestWAL_DB_AckRemovesEntry verifies Ack removes the row.
func TestWAL_DB_AckRemovesEntry(t *testing.T) {
	conn := connectPostgres(t)
	ctx := context.Background()
	_, _ = conn.Exec(ctx, "DROP TABLE IF EXISTS audit_wal_entries")

	w := waldb.New(&pgxWALDB{conn: conn})
	if err := w.EnsureTable(ctx); err != nil {
		t.Fatalf("EnsureTable: %v", err)
	}

	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		ids[i] = uniqueID("dbwal")
		entry := port.WALEntry{ID: ids[i]}
		if err := w.Append(ctx, entry); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	if err := w.Ack(ctx, ids[0]); err != nil {
		t.Fatalf("Ack: %v", err)
	}

	entries, err := w.Replay(ctx)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2, got %d", len(entries))
	}
}

// WD-004: TestWAL_DB_Recover_E2E simulates a crash by skipping Ack, then
// verifies un-acked entries are still available for replay.
func TestWAL_DB_Recover_E2E(t *testing.T) {
	conn := connectPostgres(t)
	ctx := context.Background()
	_, _ = conn.Exec(ctx, "DROP TABLE IF EXISTS audit_wal_entries")

	w := waldb.New(&pgxWALDB{conn: conn})
	if err := w.EnsureTable(ctx); err != nil {
		t.Fatalf("EnsureTable: %v", err)
	}

	entryID := uniqueID("dbwal-crash")
	entry := port.WALEntry{ID: entryID}
	if err := w.Append(ctx, entry); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Simulate crash: create a new WAL instance (no ack from previous).
	w2 := waldb.New(&pgxWALDB{conn: conn})
	entries, err := w2.Replay(ctx)
	if err != nil {
		t.Fatalf("Replay after crash: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 unacked entry, got %d", len(entries))
	}
	if entries[0].ID != entryID {
		t.Fatalf("expected %q, got %q", entryID, entries[0].ID)
	}
}
