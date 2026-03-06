package clickhouse

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

// mockOLAP records batch inserts for assertions.
type mockOLAP struct {
	mu       sync.Mutex
	batches  [][]domain.VersionRecord
	failNext int32 // atomic: number of times to fail before succeeding
}

func (m *mockOLAP) BatchInsert(_ context.Context, records []domain.VersionRecord) error {
	if atomic.AddInt32(&m.failNext, -1) >= 0 {
		return errors.New("connection refused")
	}
	m.mu.Lock()
	cp := make([]domain.VersionRecord, len(records))
	copy(cp, records)
	m.batches = append(m.batches, cp)
	m.mu.Unlock()
	return nil
}

func (m *mockOLAP) Query(_ context.Context, _ string, _ ...port.ListOption) ([]domain.VersionRecord, error) {
	return nil, nil
}

func (m *mockOLAP) allRecords() []domain.VersionRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	var all []domain.VersionRecord
	for _, b := range m.batches {
		all = append(all, b...)
	}
	return all
}

func (m *mockOLAP) batchCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.batches)
}

func makeRecord(id string) domain.VersionRecord {
	return domain.VersionRecord{
		ID:         id,
		EntityType: "user",
		EntityID:   id,
		Version:    1,
		CreatedAt:  time.Now(),
	}
}

func TestBatchBuffer_FlushOnSize(t *testing.T) {
	sink := &mockOLAP{failNext: -1} // never fail
	buf := NewBatchBuffer(sink, BatchConfig{
		BatchSize:     3,
		FlushInterval: 10 * time.Second, // large enough not to fire
		MaxRetries:    1,
		BaseBackoff:   time.Millisecond,
	})

	// Enqueue exactly 3 records — should trigger flush immediately.
	buf.Enqueue(makeRecord("a"))
	buf.Enqueue(makeRecord("b"))
	buf.Enqueue(makeRecord("c"))

	// Give the goroutine a moment to process.
	time.Sleep(50 * time.Millisecond)

	if got := sink.batchCount(); got < 1 {
		t.Fatalf("expected at least 1 flush, got %d", got)
	}
	if got := len(sink.allRecords()); got != 3 {
		t.Fatalf("expected 3 flushed records, got %d", got)
	}

	buf.Close()
}

func TestBatchBuffer_FlushOnTimer(t *testing.T) {
	sink := &mockOLAP{failNext: -1}
	buf := NewBatchBuffer(sink, BatchConfig{
		BatchSize:     100, // won't reach this
		FlushInterval: 50 * time.Millisecond,
		MaxRetries:    1,
		BaseBackoff:   time.Millisecond,
	})

	buf.Enqueue(makeRecord("x"))

	// Wait for the timer to fire.
	time.Sleep(150 * time.Millisecond)

	if got := len(sink.allRecords()); got != 1 {
		t.Fatalf("expected 1 timer-flushed record, got %d", got)
	}

	buf.Close()
}

func TestBatchBuffer_RetryOnFailure(t *testing.T) {
	sink := &mockOLAP{failNext: 2} // fail twice, succeed on 3rd attempt
	buf := NewBatchBuffer(sink, BatchConfig{
		BatchSize:     2,
		FlushInterval: 10 * time.Second,
		MaxRetries:    3,
		BaseBackoff:   time.Millisecond, // fast backoff for tests
	})

	buf.Enqueue(makeRecord("r1"))
	buf.Enqueue(makeRecord("r2"))

	// Wait for retries to complete.
	time.Sleep(200 * time.Millisecond)

	records := sink.allRecords()
	if len(records) != 2 {
		t.Fatalf("expected 2 records after retry, got %d", len(records))
	}

	buf.Close()
}

func TestBatchBuffer_CloseFlushesRemaining(t *testing.T) {
	sink := &mockOLAP{failNext: -1}
	buf := NewBatchBuffer(sink, BatchConfig{
		BatchSize:     100,
		FlushInterval: 10 * time.Second,
		MaxRetries:    1,
		BaseBackoff:   time.Millisecond,
	})

	buf.Enqueue(makeRecord("z"))
	// Close should drain and flush the remaining record.
	buf.Close()

	if got := len(sink.allRecords()); got != 1 {
		t.Fatalf("expected 1 record after close, got %d", got)
	}
}
