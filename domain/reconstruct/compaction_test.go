package reconstruct

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
	"github.com/abhipray-cpu/go-audit/domain/registry"
)

// ==========================================================================
// Compaction test infrastructure
// ==========================================================================

// memWriter records Save calls and also updates the reader's store.
type memWriter struct {
	reader *memReader
}

func (w *memWriter) Save(_ context.Context, rec domain.VersionRecord) error {
	// Upsert: replace existing record with same version, or append.
	for i, r := range w.reader.records {
		if r.EntityType == rec.EntityType && r.EntityID == rec.EntityID && r.Version == rec.Version {
			w.reader.records[i] = rec
			return nil
		}
	}
	w.reader.records = append(w.reader.records, rec)
	return nil
}

func (w *memWriter) Update(_ context.Context, rec domain.VersionRecord) error {
	return w.Save(context.Background(), rec)
}

func (w *memWriter) SaveInTx(_ context.Context, _ port.Transaction, rec domain.VersionRecord) error {
	return w.Save(context.Background(), rec)
}

// buildDeltaChain creates a snapshot at v1 then `deltaCount` delta records.
func buildDeltaChain(entityType, entityID string, deltaCount int) (*memReader, *memWriter) {
	differ := &testDiffer{}

	base := &testEntity{ID: entityID, Name: "v1", Score: 10}
	reader := &memReader{
		records: []domain.VersionRecord{
			makeRecord(entityType, entityID, 1, domain.StrategyFull, base, nil),
		},
	}
	writer := &memWriter{reader: reader}

	prev := base
	for i := 2; i <= deltaCount+1; i++ {
		curr := &testEntity{ID: entityID, Name: fmt.Sprintf("v%d", i), Score: i * 10}
		delta, _ := differ.Diff(context.Background(), prev, curr)
		reader.records = append(reader.records, makeRecord(entityType, entityID, int64(i), domain.StrategyDelta, curr, &delta))
		prev = curr
	}

	return reader, writer
}

// ==========================================================================
// GA-036 — Compaction Engine Tests
// ==========================================================================

func TestCompaction_BoundsDeltaChain(t *testing.T) {
	// 60 deltas (v2–v61) after snapshot at v1. MaxDeltaChain=50.
	// Expected: compaction snapshot at v51 (50th delta), chain resets.
	reader, writer := buildDeltaChain("testentity", "e1", 60)

	reg := registry.New()
	reg.Register(&testEntity{})

	compactor := NewCompactor(CompactorConfig{
		Reader:        reader,
		Writer:        writer,
		Serializer:    &testSerializer{},
		Differ:        &testDiffer{},
		Registry:      reg,
		MaxDeltaChain: 50,
	})

	compacted, err := compactor.Compact(context.Background(), "testentity", "e1")
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}

	if compacted != 1 {
		t.Errorf("expected 1 compaction snapshot, got %d", compacted)
	}

	// Verify: v51 should now be StrategyFull.
	for _, r := range reader.records {
		if r.Version == 51 {
			if r.Strategy != domain.StrategyFull {
				t.Errorf("v51 strategy = %v, want Full (compacted)", r.Strategy)
			}
			// Verify the data is correct.
			var entity testEntity
			json.Unmarshal(r.Data, &entity)
			if entity.Name != "v51" {
				t.Errorf("v51 Name = %q, want %q", entity.Name, "v51")
			}
			if entity.Score != 510 {
				t.Errorf("v51 Score = %d, want %d", entity.Score, 510)
			}
			break
		}
	}
}

func TestCompaction_ReconstructionCorrect(t *testing.T) {
	// After compaction, reconstruction should still produce the same result.
	reader, writer := buildDeltaChain("testentity", "e1", 60)

	reg := registry.New()
	reg.Register(&testEntity{})

	ser := &testSerializer{}
	differ := &testDiffer{}

	engine := NewEngine(reader, ser, differ, reg)

	// Reconstruct v61 before compaction.
	beforeResult, err := engine.Reconstruct(context.Background(), "testentity", "e1", 61)
	if err != nil {
		t.Fatalf("Reconstruct before compaction error: %v", err)
	}
	beforeEntity := beforeResult.(*testEntity)

	// Run compaction.
	compactor := NewCompactor(CompactorConfig{
		Reader:        reader,
		Writer:        writer,
		Serializer:    ser,
		Differ:        differ,
		Registry:      reg,
		MaxDeltaChain: 50,
	})

	_, err = compactor.Compact(context.Background(), "testentity", "e1")
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}

	// Reconstruct v61 after compaction — should match.
	afterResult, err := engine.Reconstruct(context.Background(), "testentity", "e1", 61)
	if err != nil {
		t.Fatalf("Reconstruct after compaction error: %v", err)
	}
	afterEntity := afterResult.(*testEntity)

	if beforeEntity.Name != afterEntity.Name {
		t.Errorf("Name mismatch: before=%q, after=%q", beforeEntity.Name, afterEntity.Name)
	}
	if beforeEntity.Score != afterEntity.Score {
		t.Errorf("Score mismatch: before=%d, after=%d", beforeEntity.Score, afterEntity.Score)
	}
}

func TestCompaction_NoDeltasNoOp(t *testing.T) {
	// All full snapshots — no compaction needed.
	reader := &memReader{}
	writer := &memWriter{reader: reader}

	for i := 1; i <= 10; i++ {
		entity := &testEntity{ID: "e1", Name: fmt.Sprintf("v%d", i), Score: i * 10}
		reader.records = append(reader.records, makeRecord("testentity", "e1", int64(i), domain.StrategyFull, entity, nil))
	}

	reg := registry.New()
	reg.Register(&testEntity{})

	compactor := NewCompactor(CompactorConfig{
		Reader:        reader,
		Writer:        writer,
		Serializer:    &testSerializer{},
		Differ:        &testDiffer{},
		Registry:      reg,
		MaxDeltaChain: 50,
	})

	compacted, err := compactor.Compact(context.Background(), "testentity", "e1")
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}
	if compacted != 0 {
		t.Errorf("expected 0 compactions, got %d", compacted)
	}
}

func TestCompaction_ShortChainNoOp(t *testing.T) {
	// 30 deltas < MaxDeltaChain=50 → no compaction.
	reader, writer := buildDeltaChain("testentity", "e1", 30)

	reg := registry.New()
	reg.Register(&testEntity{})

	compactor := NewCompactor(CompactorConfig{
		Reader:        reader,
		Writer:        writer,
		Serializer:    &testSerializer{},
		Differ:        &testDiffer{},
		Registry:      reg,
		MaxDeltaChain: 50,
	})

	compacted, err := compactor.Compact(context.Background(), "testentity", "e1")
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}
	if compacted != 0 {
		t.Errorf("expected 0 compactions, got %d", compacted)
	}
}

func TestCompaction_MultipleBoundaries(t *testing.T) {
	// 110 deltas with MaxDeltaChain=50 → compaction at v51 and v101.
	reader, writer := buildDeltaChain("testentity", "e1", 110)

	reg := registry.New()
	reg.Register(&testEntity{})

	compactor := NewCompactor(CompactorConfig{
		Reader:        reader,
		Writer:        writer,
		Serializer:    &testSerializer{},
		Differ:        &testDiffer{},
		Registry:      reg,
		MaxDeltaChain: 50,
	})

	compacted, err := compactor.Compact(context.Background(), "testentity", "e1")
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}
	if compacted != 2 {
		t.Errorf("expected 2 compaction snapshots, got %d", compacted)
	}
}
