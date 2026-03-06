package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------- in-memory database mock ----------

// memDB is a thread-safe in-memory implementation of WALDB for unit tests.
type memDB struct {
	mu   sync.Mutex
	rows []memRow // ordered by insert time
}

type memRow struct {
	id        string
	payload   []byte
	createdAt time.Time
}

func newMemDB() *memDB { return &memDB{} }

func (m *memDB) ExecContext(_ context.Context, query string, args ...any) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch {
	case strings.Contains(query, "CREATE TABLE"):
		return &memResult{0}, nil

	case strings.Contains(query, "INSERT"):
		id := args[0].(string)
		payload := args[1].([]byte)
		// Check for duplicate primary key.
		for _, r := range m.rows {
			if r.id == id {
				return nil, fmt.Errorf("UNIQUE constraint violated: id=%s", id)
			}
		}
		m.rows = append(m.rows, memRow{id: id, payload: payload, createdAt: time.Now()})
		return &memResult{1}, nil

	case strings.Contains(query, "DELETE"):
		id := args[0].(string)
		for i, r := range m.rows {
			if r.id == id {
				m.rows = append(m.rows[:i], m.rows[i+1:]...)
				return &memResult{1}, nil
			}
		}
		return &memResult{0}, nil

	default:
		return &memResult{0}, nil
	}
}

func (m *memDB) QueryContext(_ context.Context, _ string, _ ...any) (Rows, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Return a copy so iteration doesn't hold the lock.
	cp := make([]memRow, len(m.rows))
	copy(cp, m.rows)
	return &memRows{rows: cp, idx: -1}, nil
}

type memResult struct{ affected int64 }

func (r *memResult) RowsAffected() (int64, error) { return r.affected, nil }

type memRows struct {
	rows []memRow
	idx  int
}

func (r *memRows) Next() bool {
	r.idx++
	return r.idx < len(r.rows)
}

func (r *memRows) Scan(dest ...any) error {
	if r.idx >= len(r.rows) {
		return fmt.Errorf("memRows: no current row")
	}
	p, ok := dest[0].(*[]byte)
	if !ok {
		return fmt.Errorf("memRows: expected *[]byte, got %T", dest[0])
	}
	*p = r.rows[r.idx].payload
	return nil
}

func (r *memRows) Close() error { return nil }
func (r *memRows) Err() error   { return nil }

// ---------- recording writer ----------

type recordingWriter struct {
	mu      sync.Mutex
	records []domain.VersionRecord
}

func (w *recordingWriter) Save(_ context.Context, rec domain.VersionRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, r := range w.records {
		if r.EntityType == rec.EntityType && r.EntityID == rec.EntityID && r.Version == rec.Version {
			return domain.ErrDuplicateVersion
		}
	}
	w.records = append(w.records, rec)
	return nil
}

func (w *recordingWriter) Update(_ context.Context, rec domain.VersionRecord) error {
	return w.Save(context.Background(), rec)
}

func (w *recordingWriter) SaveInTx(_ context.Context, _ port.Transaction, rec domain.VersionRecord) error {
	return w.Save(context.Background(), rec)
}

// ---------- Tests ----------

func TestDBWAL_AppendAndReplay(t *testing.T) {
	db := newMemDB()
	wal := New(db)
	ctx := context.Background()

	if err := wal.EnsureTable(ctx); err != nil {
		t.Fatalf("EnsureTable: %v", err)
	}

	entries := []port.WALEntry{
		{ID: "e1", Record: domain.VersionRecord{EntityType: "user", EntityID: "u1", Version: 1}, CreatedAt: time.Now()},
		{ID: "e2", Record: domain.VersionRecord{EntityType: "user", EntityID: "u2", Version: 1}, CreatedAt: time.Now()},
		{ID: "e3", Record: domain.VersionRecord{EntityType: "order", EntityID: "o1", Version: 1}, CreatedAt: time.Now()},
	}

	for _, e := range entries {
		if err := wal.Append(ctx, e); err != nil {
			t.Fatalf("Append(%s): %v", e.ID, err)
		}
	}

	replayed, err := wal.Replay(ctx)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(replayed) != 3 {
		t.Fatalf("Replay returned %d entries, want 3", len(replayed))
	}
	for i, r := range replayed {
		if r.ID != entries[i].ID {
			t.Errorf("entry[%d].ID = %s, want %s", i, r.ID, entries[i].ID)
		}
	}
}

func TestDBWAL_AckDeletes(t *testing.T) {
	db := newMemDB()
	wal := New(db)
	ctx := context.Background()
	_ = wal.EnsureTable(ctx)

	_ = wal.Append(ctx, port.WALEntry{ID: "e1", Record: domain.VersionRecord{EntityType: "x", EntityID: "1", Version: 1}})
	_ = wal.Append(ctx, port.WALEntry{ID: "e2", Record: domain.VersionRecord{EntityType: "x", EntityID: "2", Version: 1}})
	_ = wal.Append(ctx, port.WALEntry{ID: "e3", Record: domain.VersionRecord{EntityType: "x", EntityID: "3", Version: 1}})

	// Ack middle entry.
	if err := wal.Ack(ctx, "e2"); err != nil {
		t.Fatalf("Ack: %v", err)
	}

	replayed, err := wal.Replay(ctx)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(replayed) != 2 {
		t.Fatalf("after Ack, Replay returned %d entries, want 2", len(replayed))
	}
	ids := make(map[string]bool)
	for _, r := range replayed {
		ids[r.ID] = true
	}
	if ids["e2"] {
		t.Error("acked entry e2 should not appear in replay")
	}
	if !ids["e1"] || !ids["e3"] {
		t.Error("expected e1 and e3 in replay")
	}
}

func TestDBWAL_MultiPod(t *testing.T) {
	// Two WAL instances sharing the same DB simulate multi-pod.
	db := newMemDB()
	wal1 := New(db)
	wal2 := New(db)
	ctx := context.Background()
	_ = wal1.EnsureTable(ctx)

	_ = wal1.Append(ctx, port.WALEntry{ID: "pod1-e1", Record: domain.VersionRecord{EntityType: "u", EntityID: "1", Version: 1}})
	_ = wal2.Append(ctx, port.WALEntry{ID: "pod2-e1", Record: domain.VersionRecord{EntityType: "u", EntityID: "2", Version: 1}})

	// Duplicate ID should fail (UNIQUE constraint).
	err := wal2.Append(ctx, port.WALEntry{ID: "pod1-e1", Record: domain.VersionRecord{EntityType: "u", EntityID: "3", Version: 1}})
	if err == nil {
		t.Fatal("expected UNIQUE violation for duplicate WAL entry ID, got nil")
	}

	// Both pods' entries visible in replay.
	replayed, _ := wal1.Replay(ctx)
	if len(replayed) != 2 {
		t.Fatalf("Replay returned %d entries, want 2", len(replayed))
	}
}

func TestDBWAL_SchemaCreated(t *testing.T) {
	db := newMemDB()
	wal := New(db)
	ctx := context.Background()

	if err := wal.EnsureTable(ctx); err != nil {
		t.Fatalf("EnsureTable: %v", err)
	}

	// Calling it again should be idempotent.
	if err := wal.EnsureTable(ctx); err != nil {
		t.Fatalf("EnsureTable (second call): %v", err)
	}

	// Verify we can append (table exists).
	err := wal.Append(ctx, port.WALEntry{
		ID:     "test-1",
		Record: domain.VersionRecord{EntityType: "t", EntityID: "1", Version: 1},
	})
	if err != nil {
		t.Fatalf("Append after EnsureTable: %v", err)
	}
}

func TestDBWAL_RecoverReplayAndPersist(t *testing.T) {
	db := newMemDB()
	wal := New(db)
	ctx := context.Background()
	_ = wal.EnsureTable(ctx)

	// Simulate crash: entries appended but never acked.
	_ = wal.Append(ctx, port.WALEntry{ID: "r1", Record: domain.VersionRecord{EntityType: "user", EntityID: "u1", Version: 1}})
	_ = wal.Append(ctx, port.WALEntry{ID: "r2", Record: domain.VersionRecord{EntityType: "user", EntityID: "u2", Version: 1}})

	writer := &recordingWriter{}

	result, err := wal.Recover(ctx, writer)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if result.Total() != 2 {
		t.Errorf("recovered total = %d, want 2", result.Total())
	}
	if result.Replayed != 2 {
		t.Errorf("recovered replayed = %d, want 2", result.Replayed)
	}

	// Writer should have received both records.
	writer.mu.Lock()
	count := len(writer.records)
	writer.mu.Unlock()
	if count != 2 {
		t.Errorf("writer has %d records, want 2", count)
	}

	// After recovery, replay should return nothing.
	replayed, _ := wal.Replay(ctx)
	if len(replayed) != 0 {
		t.Errorf("after Recover, Replay returned %d entries, want 0", len(replayed))
	}
}

func TestDBWAL_IdempotentReplay(t *testing.T) {
	db := newMemDB()
	wal := New(db)
	ctx := context.Background()
	_ = wal.EnsureTable(ctx)

	_ = wal.Append(ctx, port.WALEntry{ID: "i1", Record: domain.VersionRecord{EntityType: "user", EntityID: "u1", Version: 1}})

	writer := &recordingWriter{}

	// First recovery.
	_, _ = wal.Recover(ctx, writer)

	// Re-append same entry (simulating double-write after partial crash).
	_ = wal.Append(ctx, port.WALEntry{ID: "i2", Record: domain.VersionRecord{EntityType: "user", EntityID: "u1", Version: 1}})

	// Second recovery: same entity/version → duplicate → should still succeed.
	result, err := wal.Recover(ctx, writer)
	if err != nil {
		t.Fatalf("second Recover: %v", err)
	}
	if result.Total() != 1 {
		t.Errorf("second recover total = %d, want 1", result.Total())
	}
	if result.Skipped != 1 {
		t.Errorf("second recover skipped = %d, want 1 (duplicate entry)", result.Skipped)
	}
}

func TestDBWAL_ConcurrentAppends(t *testing.T) {
	db := newMemDB()
	wal := New(db)
	ctx := context.Background()
	_ = wal.EnsureTable(ctx)

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			entry := port.WALEntry{
				ID:     fmt.Sprintf("concurrent-%d", id),
				Record: domain.VersionRecord{EntityType: "user", EntityID: fmt.Sprintf("u%d", id), Version: 1},
			}
			if err := wal.Append(ctx, entry); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent Append error: %v", err)
	}

	replayed, err := wal.Replay(ctx)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(replayed) != goroutines {
		t.Errorf("Replay returned %d entries, want %d", len(replayed), goroutines)
	}
}

func TestDBWAL_ConcurrentAppendAndAck(t *testing.T) {
	db := newMemDB()
	wal := New(db)
	ctx := context.Background()
	_ = wal.EnsureTable(ctx)

	// Pre-populate.
	for i := 0; i < 10; i++ {
		_ = wal.Append(ctx, port.WALEntry{
			ID:     fmt.Sprintf("ca-%d", i),
			Record: domain.VersionRecord{EntityType: "x", EntityID: fmt.Sprintf("e%d", i), Version: 1},
		})
	}

	var wg sync.WaitGroup

	// Concurrent acks for even entries.
	for i := 0; i < 10; i += 2 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = wal.Ack(ctx, fmt.Sprintf("ca-%d", id))
		}(i)
	}

	// Concurrent appends.
	for i := 10; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = wal.Append(ctx, port.WALEntry{
				ID:     fmt.Sprintf("ca-%d", id),
				Record: domain.VersionRecord{EntityType: "x", EntityID: fmt.Sprintf("e%d", id), Version: 1},
			})
		}(i)
	}

	wg.Wait()

	replayed, err := wal.Replay(ctx)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	// 10 original - 5 acked + 10 new = 15
	if len(replayed) != 15 {
		t.Errorf("Replay returned %d entries, want 15", len(replayed))
	}
}

func BenchmarkDBWAL_Append(b *testing.B) {
	db := newMemDB()
	wal := New(db)
	ctx := context.Background()
	_ = wal.EnsureTable(ctx)

	entry := port.WALEntry{
		ID:     "bench",
		Record: domain.VersionRecord{EntityType: "user", EntityID: "u1", Version: 1, Data: []byte(`{"name":"bench"}`)},
	}

	// Pre-marshal to exclude from timing.
	payload, _ := json.Marshal(entry)
	_ = payload

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := entry
		e.ID = fmt.Sprintf("bench-%d", i)
		_ = wal.Append(ctx, e)
	}
}

// ---------- helpers ----------

// Verify isDuplicateErr works with errors.Is semantics.
func TestIsDuplicateErr(t *testing.T) {
	if !isDuplicateErr(domain.ErrDuplicateVersion) {
		t.Error("expected true for ErrDuplicateVersion")
	}
	if !isDuplicateErr(fmt.Errorf("wrap: %w", domain.ErrDuplicateVersion)) {
		t.Error("expected true for wrapped ErrDuplicateVersion")
	}
	// Double-wrapped.
	if !isDuplicateErr(fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", domain.ErrDuplicateVersion))) {
		t.Error("expected true for double-wrapped ErrDuplicateVersion")
	}
	if isDuplicateErr(errors.New("some other error")) {
		t.Error("expected false for unrelated error")
	}
	if isDuplicateErr(nil) {
		t.Error("expected false for nil")
	}
}
