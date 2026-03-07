// Package db provides a database-backed WAL adapter for go-audit.
//
// DBWALAdapter uses a staging table in the primary database for WAL entries.
// This is the recommended WAL for multi-pod deployments where a shared
// database is available. Entries are INSERT-ed on Append, DELETE-d on Ack,
// and SELECT-ed on Replay.
//
// # Supported Databases
//
// This adapter uses PostgreSQL-style $1, $2 positional placeholders.
// It is compatible with PostgreSQL and CockroachDB. For MySQL, use the
// adapter/mysql package or a separate MySQL WAL implementation.
//
// # Schema
//
// The adapter auto-creates the table on first use (via [EnsureTable]):
//
//	CREATE TABLE IF NOT EXISTS audit_wal_entries (
//	    id         TEXT PRIMARY KEY,
//	    payload    BYTEA NOT NULL,
//	    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
//	)
//
// # Thread Safety
//
// All methods are safe for concurrent use. Write methods acquire an exclusive
// lock; [WAL.Replay] acquires a read lock to allow concurrent reads.
//
// # Recovery
//
// On startup, call [WAL.Recover] to replay un-acknowledged entries from a
// previous crash. The method is idempotent — duplicate versions are detected
// and skipped automatically.
package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time interface check.
var _ port.WALPort = (*WAL)(nil)

// WALDB abstracts the database operations needed by the DB-backed WAL.
// Both *sql.DB and *pgx.Conn (via adapter) satisfy this interface.
type WALDB interface {
	ExecContext(ctx context.Context, query string, args ...any) (Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (Rows, error)
}

// Result is the minimal interface for a database exec result.
type Result interface {
	RowsAffected() (int64, error)
}

// Rows is the minimal interface for iterating database rows.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Close() error
	Err() error
}

// WAL is a database-backed Write-Ahead Log. It stores WAL entries as rows
// in the audit_wal_entries table. Create one with [New].
type WAL struct {
	mu sync.RWMutex
	db WALDB
}

// New creates a DB-backed WAL using the provided database handle.
func New(db WALDB) *WAL {
	return &WAL{db: db}
}

// EnsureTable idempotently creates the audit_wal_entries staging table.
// Call this once at application startup.
func (w *WAL) EnsureTable(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err := w.db.ExecContext(ctx, ddlWALTable)
	if err != nil {
		return fmt.Errorf("dbwal: create table: %w", err)
	}
	return nil
}

const ddlWALTable = `
CREATE TABLE IF NOT EXISTS audit_wal_entries (
    id         TEXT        PRIMARY KEY,
    payload    BYTEA       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

// Append writes a WAL entry to the staging table.
func (w *WAL) Append(ctx context.Context, entry port.WALEntry) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("dbwal: marshal entry: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	_, err = w.db.ExecContext(ctx,
		`INSERT INTO audit_wal_entries (id, payload) VALUES ($1, $2)`,
		entry.ID, payload,
	)
	if err != nil {
		return fmt.Errorf("dbwal: insert entry %s: %w", entry.ID, err)
	}
	return nil
}

// Ack acknowledges a WAL entry by deleting it from the staging table.
func (w *WAL) Ack(ctx context.Context, entryID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err := w.db.ExecContext(ctx,
		`DELETE FROM audit_wal_entries WHERE id = $1`,
		entryID,
	)
	if err != nil {
		return fmt.Errorf("dbwal: ack entry %s: %w", entryID, err)
	}
	return nil
}

// Replay returns all un-acknowledged WAL entries from the staging table,
// ordered by creation time (oldest first) for deterministic recovery.
func (w *WAL) Replay(ctx context.Context) ([]port.WALEntry, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	rows, err := w.db.QueryContext(ctx,
		`SELECT payload FROM audit_wal_entries ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("dbwal: replay query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []port.WALEntry
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("dbwal: scan row: %w", err)
		}
		var entry port.WALEntry
		if err := json.Unmarshal(payload, &entry); err != nil {
			return nil, fmt.Errorf("dbwal: unmarshal entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dbwal: rows iteration: %w", err)
	}
	return entries, nil
}

// RecoverResult summarises what happened during recovery.
type RecoverResult struct {
	// Replayed is the number of entries successfully saved and acked.
	Replayed int
	// Skipped is the number of entries that were already persisted
	// (duplicate) and were acked without re-saving.
	Skipped int
	// AckErrors is the number of entries where the duplicate was detected
	// but the subsequent Ack failed. These entries remain in the WAL and
	// will be retried on the next Recover call.
	AckErrors int
}

// Total returns the total number of entries processed (Replayed + Skipped + AckErrors).
func (r RecoverResult) Total() int { return r.Replayed + r.Skipped + r.AckErrors }

// Recover replays all un-acknowledged entries and persists them via the
// provided writer, then acks each one. This is the startup recovery
// protocol for crash recovery.
//
// The returned RecoverResult distinguishes between entries that were
// freshly saved (Replayed) and entries that were already persisted and
// skipped as duplicates (Skipped).
func (w *WAL) Recover(ctx context.Context, writer port.VersionWriterPort) (RecoverResult, error) {
	entries, err := w.Replay(ctx)
	if err != nil {
		return RecoverResult{}, fmt.Errorf("dbwal: recover replay: %w", err)
	}

	var result RecoverResult
	for _, entry := range entries {
		if err := writer.Save(ctx, entry.Record); err != nil {
			// Skip duplicates (idempotent replay).
			if isDuplicateErr(err) {
				if ackErr := w.Ack(ctx, entry.ID); ackErr != nil {
					// Entry stays in WAL, will be retried on next Recover.
					result.AckErrors++
					continue
				}
				result.Skipped++
				continue
			}
			return result, fmt.Errorf("dbwal: recover save %s: %w", entry.ID, err)
		}
		if err := w.Ack(ctx, entry.ID); err != nil {
			return result, fmt.Errorf("dbwal: recover ack %s: %w", entry.ID, err)
		}
		result.Replayed++
	}
	return result, nil
}

// isDuplicateErr checks if an error wraps ErrDuplicateVersion anywhere in
// the error chain, including joined errors (Go 1.20+ Unwrap() []error).
func isDuplicateErr(err error) bool {
	return errors.Is(err, domain.ErrDuplicateVersion)
}
