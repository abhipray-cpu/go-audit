package reconstruct

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
	"github.com/abhipray-cpu/go-audit/domain/registry"
)

// ==========================================================================
// Test infrastructure
// ==========================================================================

type testEntity struct {
	ID    string `version:"id" json:"id"`
	Name  string `version:"tracked" json:"name"`
	Score int    `version:"tracked" json:"score"`
}

type testSerializer struct{}

func (s *testSerializer) Marshal(_ context.Context, v any) ([]byte, error) {
	return json.Marshal(v)
}
func (s *testSerializer) Unmarshal(_ context.Context, data []byte, v any) error {
	return json.Unmarshal(data, v)
}
func (s *testSerializer) ContentType() string { return "application/json" }

type testDiffer struct{}

func (d *testDiffer) Diff(_ context.Context, prev, curr any) (domain.Delta, error) {
	p, _ := json.Marshal(prev)
	c, _ := json.Marshal(curr)

	var pm, cm map[string]json.RawMessage
	json.Unmarshal(p, &pm)
	json.Unmarshal(c, &cm)

	var changes []domain.FieldChange
	for k, cv := range cm {
		pv, exists := pm[k]
		if !exists || string(pv) != string(cv) {
			changes = append(changes, domain.FieldChange{
				Path:     k,
				OldValue: pv,
				NewValue: cv,
			})
		}
	}
	return domain.Delta{Changes: changes}, nil
}

func (d *testDiffer) Apply(_ context.Context, prev any, delta domain.Delta) (any, error) {
	// Marshal prev to map, apply changes, unmarshal back.
	data, _ := json.Marshal(prev)
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)

	for _, ch := range delta.Changes {
		if ch.NewValue == nil {
			delete(m, ch.Path)
		} else {
			m[ch.Path] = ch.NewValue
		}
	}

	result, _ := json.Marshal(m)
	out := &testEntity{}
	json.Unmarshal(result, out)
	return out, nil
}

// memReader stores records in memory.
type memReader struct {
	records []domain.VersionRecord
}

func (m *memReader) GetByVersion(_ context.Context, entityType, entityID string, ver int64) (domain.VersionRecord, error) {
	for _, r := range m.records {
		if r.EntityType == entityType && r.EntityID == entityID && r.Version == ver {
			return r, nil
		}
	}
	return domain.VersionRecord{}, domain.ErrNotFound
}

func (m *memReader) GetLatest(_ context.Context, entityType, entityID string) (domain.VersionRecord, error) {
	var best *domain.VersionRecord
	for i := range m.records {
		r := &m.records[i]
		if r.EntityType == entityType && r.EntityID == entityID {
			if best == nil || r.Version > best.Version {
				best = r
			}
		}
	}
	if best == nil {
		return domain.VersionRecord{}, domain.ErrNotFound
	}
	return *best, nil
}

func (m *memReader) GetAtTime(_ context.Context, _, _ string, _ time.Time) (domain.VersionRecord, error) {
	return domain.VersionRecord{}, domain.ErrNotFound
}

func (m *memReader) ListVersions(_ context.Context, entityType, entityID string, opts ...port.ListOption) ([]domain.VersionRecord, error) {
	var result []domain.VersionRecord
	for _, r := range m.records {
		if r.EntityType == entityType && r.EntityID == entityID {
			result = append(result, r)
		}
	}
	return result, nil
}

func makeRecord(entityType, entityID string, ver int64, strategy domain.Strategy, entity *testEntity, delta *domain.Delta) domain.VersionRecord {
	data, _ := json.Marshal(entity)
	return domain.VersionRecord{
		ID:         fmt.Sprintf("rec-%d", ver),
		EntityType: entityType,
		EntityID:   entityID,
		Version:    ver,
		Strategy:   strategy,
		Data:       data,
		Delta:      delta,
		CreatedAt:  time.Now(),
	}
}

func setupEngine(records []domain.VersionRecord) *Engine {
	reader := &memReader{records: records}
	ser := &testSerializer{}
	differ := &testDiffer{}

	reg := registry.New()
	reg.Register(&testEntity{})

	return NewEngine(reader, ser, differ, reg)
}

// ==========================================================================
// GA-035 — Reconstruction Engine Tests
// ==========================================================================

func TestReconstruction_FullSnapshot(t *testing.T) {
	// Single full snapshot — reconstruct directly.
	entity := &testEntity{ID: "e1", Name: "Alice", Score: 100}
	records := []domain.VersionRecord{
		makeRecord("testentity", "e1", 1, domain.StrategyFull, entity, nil),
	}

	engine := setupEngine(records)
	result, err := engine.Reconstruct(context.Background(), "testentity", "e1", 1)
	if err != nil {
		t.Fatalf("Reconstruct() error: %v", err)
	}

	got := result.(*testEntity)
	if got.Name != "Alice" {
		t.Errorf("Name = %q, want %q", got.Name, "Alice")
	}
	if got.Score != 100 {
		t.Errorf("Score = %d, want %d", got.Score, 100)
	}
}

func TestReconstruction_FromDeltas(t *testing.T) {
	// Snapshot at v1, then 10 deltas (v2–v11). Reconstruct any version.
	ser := &testSerializer{}
	differ := &testDiffer{}

	base := &testEntity{ID: "e1", Name: "v1", Score: 0}
	records := []domain.VersionRecord{
		makeRecord("testentity", "e1", 1, domain.StrategyFull, base, nil),
	}

	prev := base
	for i := 2; i <= 11; i++ {
		curr := &testEntity{ID: "e1", Name: fmt.Sprintf("v%d", i), Score: i * 10}
		delta, _ := differ.Diff(context.Background(), prev, curr)

		// Data always stores full for deserialization fallback,
		// but strategy is Delta with a delta attached.
		records = append(records, makeRecord("testentity", "e1", int64(i), domain.StrategyDelta, curr, &delta))
		prev = curr
	}

	reader := &memReader{records: records}
	reg := registry.New()
	reg.Register(&testEntity{})
	engine := NewEngine(reader, ser, differ, reg)

	// Reconstruct each version and verify.
	for v := int64(1); v <= 11; v++ {
		result, err := engine.Reconstruct(context.Background(), "testentity", "e1", v)
		if err != nil {
			t.Fatalf("Reconstruct(v%d) error: %v", v, err)
		}

		got := result.(*testEntity)
		expectedName := fmt.Sprintf("v%d", v)
		expectedScore := int(v) * 10
		if v == 1 {
			expectedScore = 0
		}

		if got.Name != expectedName {
			t.Errorf("v%d: Name = %q, want %q", v, got.Name, expectedName)
		}
		if got.Score != expectedScore {
			t.Errorf("v%d: Score = %d, want %d", v, got.Score, expectedScore)
		}
	}
}

func TestReconstruction_HybridStrategy(t *testing.T) {
	// Snapshots at v1, v6, v11 with deltas between.
	ser := &testSerializer{}
	differ := &testDiffer{}

	var records []domain.VersionRecord
	prev := &testEntity{ID: "e1", Name: "v1", Score: 10}

	for i := 1; i <= 12; i++ {
		curr := &testEntity{ID: "e1", Name: fmt.Sprintf("v%d", i), Score: i * 10}

		isSnapshot := (i == 1 || i == 6 || i == 11)
		if isSnapshot {
			records = append(records, makeRecord("testentity", "e1", int64(i), domain.StrategyFull, curr, nil))
		} else {
			delta, _ := differ.Diff(context.Background(), prev, curr)
			records = append(records, makeRecord("testentity", "e1", int64(i), domain.StrategyDelta, curr, &delta))
		}
		prev = curr
	}

	reader := &memReader{records: records}
	reg := registry.New()
	reg.Register(&testEntity{})
	engine := NewEngine(reader, ser, differ, reg)

	// Reconstruct v8 — should use snapshot at v6 + deltas 7,8.
	result, err := engine.Reconstruct(context.Background(), "testentity", "e1", 8)
	if err != nil {
		t.Fatalf("Reconstruct(v8) error: %v", err)
	}

	got := result.(*testEntity)
	if got.Name != "v8" {
		t.Errorf("Name = %q, want %q", got.Name, "v8")
	}
	if got.Score != 80 {
		t.Errorf("Score = %d, want %d", got.Score, 80)
	}
}

func TestReconstruction_VersionNotFound(t *testing.T) {
	engine := setupEngine(nil)
	_, err := engine.Reconstruct(context.Background(), "testentity", "e1", 99)
	if err == nil {
		t.Fatal("expected error for non-existent version")
	}
}

func TestReconstruction_UnknownEntityType(t *testing.T) {
	engine := setupEngine(nil)
	_, err := engine.Reconstruct(context.Background(), "nonexistent", "e1", 1)
	if err == nil {
		t.Fatal("expected error for unknown entity type")
	}
}

func BenchmarkReconstruction_50Deltas(b *testing.B) {
	ser := &testSerializer{}
	differ := &testDiffer{}

	base := &testEntity{ID: "e1", Name: "v1", Score: 0}
	records := []domain.VersionRecord{
		makeRecord("testentity", "e1", 1, domain.StrategyFull, base, nil),
	}

	prev := base
	for i := 2; i <= 51; i++ {
		curr := &testEntity{ID: "e1", Name: fmt.Sprintf("v%d", i), Score: i * 10}
		delta, _ := differ.Diff(context.Background(), prev, curr)
		records = append(records, makeRecord("testentity", "e1", int64(i), domain.StrategyDelta, curr, &delta))
		prev = curr
	}

	reader := &memReader{records: records}
	reg := registry.New()
	reg.Register(&testEntity{})
	engine := NewEngine(reader, ser, differ, reg)

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.Reconstruct(ctx, "testentity", "e1", 51)
		if err != nil {
			b.Fatal(err)
		}
	}
}
