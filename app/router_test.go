package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// --- test doubles ---

type recordingWriter struct {
	mu      sync.Mutex
	records []domain.VersionRecord
}

func (w *recordingWriter) Save(_ context.Context, rec domain.VersionRecord) error {
	w.mu.Lock()
	w.records = append(w.records, rec)
	w.mu.Unlock()
	return nil
}

func (w *recordingWriter) Update(_ context.Context, rec domain.VersionRecord) error {
	return w.Save(context.Background(), rec)
}

func (w *recordingWriter) SaveInTx(_ context.Context, _ port.Transaction, rec domain.VersionRecord) error {
	return w.Save(context.Background(), rec)
}

func (w *recordingWriter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.records)
}

type recordingOLAP struct {
	mu       sync.Mutex
	inserted []domain.VersionRecord
	failNext int32 // atomic
}

func (o *recordingOLAP) BatchInsert(_ context.Context, records []domain.VersionRecord) error {
	if atomic.AddInt32(&o.failNext, -1) >= 0 {
		return errors.New("olap down")
	}
	o.mu.Lock()
	o.inserted = append(o.inserted, records...)
	o.mu.Unlock()
	return nil
}

func (o *recordingOLAP) Query(_ context.Context, _ string, _ ...port.ListOption) ([]domain.VersionRecord, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	cp := make([]domain.VersionRecord, len(o.inserted))
	copy(cp, o.inserted)
	return cp, nil
}

func (o *recordingOLAP) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.inserted)
}

func makeRec(id string) domain.VersionRecord {
	return domain.VersionRecord{
		ID:         id,
		EntityType: "user",
		EntityID:   id,
		Version:    1,
		CreatedAt:  time.Now(),
	}
}

// --- tests ---

func TestStorageRouter_WriteToBoth(t *testing.T) {
	oltp := &recordingWriter{}
	olap := &recordingOLAP{failNext: -1}
	router := NewStorageRouter(RouterConfig{OLTP: oltp, OLAP: olap})

	rec := makeRec("u1")
	if err := router.Save(context.Background(), rec); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// OLTP is synchronous — must be there immediately.
	if oltp.count() != 1 {
		t.Fatalf("expected 1 OLTP record, got %d", oltp.count())
	}

	// OLAP is async — give the goroutine a moment.
	time.Sleep(50 * time.Millisecond)
	if olap.count() != 1 {
		t.Fatalf("expected 1 OLAP record, got %d", olap.count())
	}
}

func TestStorageRouter_ReadTiered(t *testing.T) {
	oltp := &recordingWriter{}
	olap := &recordingOLAP{failNext: -1}
	router := NewStorageRouter(RouterConfig{OLTP: oltp, OLAP: olap})

	// Insert records into OLAP directly (simulating historical data).
	_ = olap.BatchInsert(context.Background(), []domain.VersionRecord{
		makeRec("h1"), makeRec("h2"), makeRec("h3"),
	})

	records, err := router.ReadFromOLAP(context.Background(), "user")
	if err != nil {
		t.Fatalf("ReadFromOLAP() error: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 OLAP records, got %d", len(records))
	}
}

func TestStorageRouter_OLAPDown_OLTPContinues(t *testing.T) {
	oltp := &recordingWriter{}
	olap := &recordingOLAP{failNext: 1} // first BatchInsert fails

	router := NewStorageRouter(RouterConfig{OLTP: oltp, OLAP: olap})

	rec := makeRec("u1")
	// Save must succeed (OLTP), even though OLAP is "down".
	if err := router.Save(context.Background(), rec); err != nil {
		t.Fatalf("Save() should succeed when OLAP is down, got: %v", err)
	}

	if oltp.count() != 1 {
		t.Fatalf("expected 1 OLTP record, got %d", oltp.count())
	}

	// OLAP failure is fire-and-forget — no records inserted.
	time.Sleep(50 * time.Millisecond)
	if olap.count() != 0 {
		t.Fatalf("expected 0 OLAP records (OLAP down), got %d", olap.count())
	}

	// "Catch up" — OLAP recovers, next write succeeds.
	rec2 := makeRec("u2")
	if err := router.Save(context.Background(), rec2); err != nil {
		t.Fatalf("Save() error on recovery: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if olap.count() != 1 {
		t.Fatalf("expected 1 OLAP record after recovery, got %d", olap.count())
	}
}
