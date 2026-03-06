package integrity

import (
	"context"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/adapter/hash"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------- in-memory store for tests ----------

type memStore struct {
	records []domain.VersionRecord
}

func (m *memStore) Save(_ context.Context, rec domain.VersionRecord) error {
	m.records = append(m.records, rec)
	return nil
}

func (m *memStore) SaveInTx(_ context.Context, _ port.Transaction, rec domain.VersionRecord) error {
	return m.Save(context.Background(), rec)
}

func (m *memStore) GetByVersion(_ context.Context, entityType, entityID string, ver int64) (domain.VersionRecord, error) {
	for _, r := range m.records {
		if r.EntityType == entityType && r.EntityID == entityID && r.Version == ver {
			return r, nil
		}
	}
	return domain.VersionRecord{}, domain.ErrNotFound
}

func (m *memStore) GetLatest(_ context.Context, entityType, entityID string) (domain.VersionRecord, error) {
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

func (m *memStore) GetAtTime(_ context.Context, _, _ string, _ time.Time) (domain.VersionRecord, error) {
	return domain.VersionRecord{}, domain.ErrNotFound
}

func (m *memStore) ListVersions(_ context.Context, entityType, entityID string, _ ...port.ListOption) ([]domain.VersionRecord, error) {
	var result []domain.VersionRecord
	for _, r := range m.records {
		if r.EntityType == entityType && r.EntityID == entityID {
			result = append(result, r)
		}
	}
	return result, nil
}

// update replaces the stored record for a specific version.
func (m *memStore) update(entityType, entityID string, ver int64, fn func(*domain.VersionRecord)) {
	for i := range m.records {
		r := &m.records[i]
		if r.EntityType == entityType && r.EntityID == entityID && r.Version == ver {
			fn(r)
			return
		}
	}
}

// ---------- tests ----------

func TestHashChain_ComputeAndStore(t *testing.T) {
	store := &memStore{}
	hasher := hash.NewSHA256()
	chain := NewChain(hasher, store)
	ctx := context.Background()

	// Create version 1.
	rec1 := domain.VersionRecord{
		ID:         "r1",
		EntityType: "user",
		EntityID:   "u1",
		Version:    1,
		Data:       []byte(`{"name":"Alice"}`),
	}
	if err := chain.ComputeAndStore(ctx, &rec1); err != nil {
		t.Fatalf("ComputeAndStore v1: %v", err)
	}
	if rec1.Hash == "" {
		t.Fatal("v1 Hash must not be empty")
	}
	if rec1.PreviousHash != "" {
		t.Fatalf("v1 PreviousHash should be empty, got %q", rec1.PreviousHash)
	}
	store.Save(ctx, rec1)

	// Create version 2.
	rec2 := domain.VersionRecord{
		ID:         "r2",
		EntityType: "user",
		EntityID:   "u1",
		Version:    2,
		Data:       []byte(`{"name":"Bob"}`),
	}
	if err := chain.ComputeAndStore(ctx, &rec2); err != nil {
		t.Fatalf("ComputeAndStore v2: %v", err)
	}
	if rec2.Hash == "" {
		t.Fatal("v2 Hash must not be empty")
	}
	if rec2.PreviousHash != rec1.Hash {
		t.Fatalf("v2 PreviousHash should be v1's Hash %q, got %q", rec1.Hash, rec2.PreviousHash)
	}
}

func TestHashChain_VerifyIntegrity(t *testing.T) {
	store := &memStore{}
	hasher := hash.NewSHA256()
	chain := NewChain(hasher, store)
	ctx := context.Background()

	// Build a 3-version chain.
	for i := int64(1); i <= 3; i++ {
		rec := domain.VersionRecord{
			ID:         "r" + string(rune('0'+i)),
			EntityType: "user",
			EntityID:   "u1",
			Version:    i,
			Data:       []byte(`{"v":` + string(rune('0'+i)) + `}`),
		}
		if err := chain.ComputeAndStore(ctx, &rec); err != nil {
			t.Fatalf("ComputeAndStore v%d: %v", i, err)
		}
		store.Save(ctx, rec)
	}

	// Verify — should pass.
	if err := chain.VerifyIntegrity(ctx, "user", "u1"); err != nil {
		t.Fatalf("VerifyIntegrity should pass for valid chain: %v", err)
	}
}

func TestHashChain_DetectTampering(t *testing.T) {
	store := &memStore{}
	hasher := hash.NewSHA256()
	chain := NewChain(hasher, store)
	ctx := context.Background()

	// Build a 3-version chain.
	for i := int64(1); i <= 3; i++ {
		rec := domain.VersionRecord{
			ID:         "r" + string(rune('0'+i)),
			EntityType: "user",
			EntityID:   "u1",
			Version:    i,
			Data:       []byte(`{"v":` + string(rune('0'+i)) + `}`),
		}
		if err := chain.ComputeAndStore(ctx, &rec); err != nil {
			t.Fatalf("ComputeAndStore v%d: %v", i, err)
		}
		store.Save(ctx, rec)
	}

	// Tamper with version 2's data.
	store.update("user", "u1", 2, func(r *domain.VersionRecord) {
		r.Data = []byte(`{"v":"TAMPERED"}`)
	})

	// Verify — should detect tampering.
	err := chain.VerifyIntegrity(ctx, "user", "u1")
	if err == nil {
		t.Fatal("VerifyIntegrity should detect tampering")
	}
	t.Logf("Tampering detected: %v", err)
}

func TestHashChain_10K_Versions(t *testing.T) {
	store := &memStore{}
	hasher := hash.NewSHA256()
	chain := NewChain(hasher, store)
	ctx := context.Background()

	const n = 10_000

	// Build a 10K-version chain.
	for i := int64(1); i <= n; i++ {
		rec := domain.VersionRecord{
			ID:         "r",
			EntityType: "user",
			EntityID:   "u1",
			Version:    i,
			Data:       []byte(`{"i":` + itoa(i) + `}`),
		}
		if err := chain.ComputeAndStore(ctx, &rec); err != nil {
			t.Fatalf("ComputeAndStore v%d: %v", i, err)
		}
		store.Save(ctx, rec)
	}

	// Verify valid chain.
	if err := chain.VerifyIntegrity(ctx, "user", "u1"); err != nil {
		t.Fatalf("10K chain should be valid: %v", err)
	}

	// Single-bit change in the middle.
	store.update("user", "u1", 5000, func(r *domain.VersionRecord) {
		// Flip one byte in the data.
		d := make([]byte, len(r.Data))
		copy(d, r.Data)
		d[0] ^= 0x01
		r.Data = d
	})

	err := chain.VerifyIntegrity(ctx, "user", "u1")
	if err == nil {
		t.Fatal("Should detect single-bit change in 10K chain")
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
