package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------- test helpers ----------

// fakeReader returns canned records for tiering tests.
type fakeReader struct {
	records []domain.VersionRecord
}

func (f *fakeReader) GetByVersion(_ context.Context, _, _ string, _ int64) (domain.VersionRecord, error) {
	return domain.VersionRecord{}, domain.ErrNotFound
}
func (f *fakeReader) GetLatest(_ context.Context, _, _ string) (domain.VersionRecord, error) {
	return domain.VersionRecord{}, domain.ErrNotFound
}
func (f *fakeReader) GetAtTime(_ context.Context, _, _ string, _ time.Time) (domain.VersionRecord, error) {
	return domain.VersionRecord{}, domain.ErrNotFound
}
func (f *fakeReader) ListVersions(_ context.Context, entityType, _ string, _ ...port.ListOption) ([]domain.VersionRecord, error) {
	var filtered []domain.VersionRecord
	for _, r := range f.records {
		if r.EntityType == entityType {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

// tierOLAP captures BatchInsert calls for tiering tests.
type tierOLAP struct {
	mu       sync.Mutex
	inserted []domain.VersionRecord
}

func (o *tierOLAP) BatchInsert(_ context.Context, records []domain.VersionRecord) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.inserted = append(o.inserted, records...)
	return nil
}
func (o *tierOLAP) Query(_ context.Context, _ string, _ ...port.ListOption) ([]domain.VersionRecord, error) {
	return nil, nil
}

// recordingColdStore captures Put calls.
type recordingColdStore struct {
	mu     sync.Mutex
	stored map[string][]byte
}

func newRecordingColdStore() *recordingColdStore {
	return &recordingColdStore{stored: make(map[string][]byte)}
}

func (c *recordingColdStore) Put(_ context.Context, key string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stored[key] = data
	return nil
}
func (c *recordingColdStore) Get(_ context.Context, key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d, ok := c.stored[key]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return d, nil
}

// ---------- Tests ----------

func TestTiering_MoveToWarm(t *testing.T) {
	now := time.Now()

	reader := &fakeReader{
		records: []domain.VersionRecord{
			{EntityType: "user", EntityID: "u1", Version: 1, Data: []byte(`{"id":"u1"}`), CreatedAt: now.AddDate(0, 0, -100)}, // 100 days old → warm
			{EntityType: "user", EntityID: "u2", Version: 1, Data: []byte(`{"id":"u2"}`), CreatedAt: now.AddDate(0, 0, -10)},  // 10 days old → stays hot
		},
	}

	olap := &tierOLAP{}
	cold := newRecordingColdStore()

	engine := &TieringEngine{
		cfg: TieringConfig{
			WarmAfterDays: 90,
			ColdAfterDays: 730,
			EntityTypes:   []string{"user"},
			Reader:        reader,
			OLAP:          olap,
			ColdStore:     cold,
		},
	}

	result := engine.RunOnce(context.Background())

	if result.MovedToWarm != 1 {
		t.Errorf("MovedToWarm = %d, want 1", result.MovedToWarm)
	}
	if result.MovedToCold != 0 {
		t.Errorf("MovedToCold = %d, want 0", result.MovedToCold)
	}

	olap.mu.Lock()
	insertCount := len(olap.inserted)
	olap.mu.Unlock()
	if insertCount != 1 {
		t.Fatalf("OLAP inserted %d records, want 1", insertCount)
	}
	if olap.inserted[0].EntityID != "u1" {
		t.Errorf("expected u1 moved to warm, got %s", olap.inserted[0].EntityID)
	}
}

func TestTiering_MoveToCold(t *testing.T) {
	now := time.Now()

	reader := &fakeReader{
		records: []domain.VersionRecord{
			{EntityType: "order", EntityID: "o1", Version: 1, Data: []byte(`{"id":"o1"}`), CreatedAt: now.AddDate(-3, 0, 0)},   // 3 years old → cold
			{EntityType: "order", EntityID: "o2", Version: 1, Data: []byte(`{"id":"o2"}`), CreatedAt: now.AddDate(0, 0, -100)}, // 100 days → warm
			{EntityType: "order", EntityID: "o3", Version: 1, Data: []byte(`{"id":"o3"}`), CreatedAt: now.AddDate(0, 0, -10)},  // 10 days → hot
		},
	}

	olap := &tierOLAP{}
	cold := newRecordingColdStore()

	engine := &TieringEngine{
		cfg: TieringConfig{
			WarmAfterDays: 90,
			ColdAfterDays: 730,
			EntityTypes:   []string{"order"},
			Reader:        reader,
			OLAP:          olap,
			ColdStore:     cold,
		},
	}

	result := engine.RunOnce(context.Background())

	if result.MovedToCold != 1 {
		t.Errorf("MovedToCold = %d, want 1", result.MovedToCold)
	}
	if result.MovedToWarm != 1 {
		t.Errorf("MovedToWarm = %d, want 1 (o2 at 100 days)", result.MovedToWarm)
	}

	cold.mu.Lock()
	coldCount := len(cold.stored)
	cold.mu.Unlock()
	if coldCount != 1 {
		t.Fatalf("ColdStore has %d entries, want 1", coldCount)
	}

	expectedKey := "order/o1/1"
	cold.mu.Lock()
	_, found := cold.stored[expectedKey]
	cold.mu.Unlock()
	if !found {
		t.Errorf("expected cold store key %q, not found", expectedKey)
	}
}

func TestTiering_BackgroundGoroutine(t *testing.T) {
	reader := &fakeReader{
		records: []domain.VersionRecord{
			{EntityType: "item", EntityID: "i1", Version: 1, Data: []byte(`{}`), CreatedAt: time.Now().AddDate(0, 0, -200)},
		},
	}

	olap := &tierOLAP{}

	engine := NewTieringEngine(TieringConfig{
		WarmAfterDays: 90,
		ColdAfterDays: 730,
		EntityTypes:   []string{"item"},
		Reader:        reader,
		OLAP:          olap,
		Interval:      50 * time.Millisecond,
	})

	// Wait for at least one tick.
	time.Sleep(200 * time.Millisecond)
	engine.Stop()

	olap.mu.Lock()
	insertCount := len(olap.inserted)
	olap.mu.Unlock()

	if insertCount == 0 {
		t.Error("expected background goroutine to move at least one record to warm")
	}
}
