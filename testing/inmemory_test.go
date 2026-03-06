package audittest

import (
	"context"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

func TestInMemoryStore_Save_And_GetLatest(t *testing.T) {
	s := NewInMemoryStore()

	rec := domain.VersionRecord{
		ID:         "r1",
		EntityType: "user",
		EntityID:   "u1",
		Version:    1,
		Data:       []byte(`{"name":"Alice"}`),
		CreatedAt:  time.Now(),
	}
	if err := s.Save(context.Background(), rec); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	got, err := s.GetLatest(context.Background(), "user", "u1")
	if err != nil {
		t.Fatalf("GetLatest() error: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("Version = %d, want 1", got.Version)
	}
}

func TestInMemoryStore_GetLatest_NotFound(t *testing.T) {
	s := NewInMemoryStore()

	_, err := s.GetLatest(context.Background(), "user", "u1")
	if err != domain.ErrNotFound {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestInMemoryStore_GetByVersion(t *testing.T) {
	s := NewInMemoryStore()

	for i := int64(1); i <= 3; i++ {
		_ = s.Save(context.Background(), domain.VersionRecord{
			ID:         "r" + string(rune('0'+i)),
			EntityType: "user",
			EntityID:   "u1",
			Version:    i,
			Data:       []byte(`{}`),
			CreatedAt:  time.Now(),
		})
	}

	got, err := s.GetByVersion(context.Background(), "user", "u1", 2)
	if err != nil {
		t.Fatalf("GetByVersion() error: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("Version = %d, want 2", got.Version)
	}

	_, err = s.GetByVersion(context.Background(), "user", "u1", 99)
	if err != domain.ErrNotFound {
		t.Errorf("expected ErrNotFound for missing version, got: %v", err)
	}
}

func TestInMemoryStore_GetAtTime(t *testing.T) {
	s := NewInMemoryStore()

	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	_ = s.Save(context.Background(), domain.VersionRecord{
		EntityType: "user", EntityID: "u1", Version: 1, CreatedAt: t1,
	})
	_ = s.Save(context.Background(), domain.VersionRecord{
		EntityType: "user", EntityID: "u1", Version: 2, CreatedAt: t2,
	})

	// Query at a time between v1 and v2 — should return v1.
	got, err := s.GetAtTime(context.Background(), "user", "u1", t1.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetAtTime() error: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("Version = %d, want 1", got.Version)
	}

	// Query after v2 — should return v2.
	got2, err := s.GetAtTime(context.Background(), "user", "u1", t2.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetAtTime() error: %v", err)
	}
	if got2.Version != 2 {
		t.Errorf("Version = %d, want 2", got2.Version)
	}
}

func TestInMemoryStore_ListVersions(t *testing.T) {
	s := NewInMemoryStore()

	for i := int64(1); i <= 5; i++ {
		_ = s.Save(context.Background(), domain.VersionRecord{
			EntityType: "user", EntityID: "u1", Version: i, CreatedAt: time.Now(),
		})
	}

	// Default: descending, limit 50.
	recs, err := s.ListVersions(context.Background(), "user", "u1")
	if err != nil {
		t.Fatalf("ListVersions() error: %v", err)
	}
	if len(recs) != 5 {
		t.Fatalf("expected 5 records, got %d", len(recs))
	}
	if recs[0].Version != 5 {
		t.Errorf("first record Version = %d, want 5 (descending)", recs[0].Version)
	}

	// Ascending, limit 2.
	recs2, err := s.ListVersions(context.Background(), "user", "u1",
		port.WithOrderAsc(), port.WithLimit(2))
	if err != nil {
		t.Fatalf("ListVersions(asc, limit 2) error: %v", err)
	}
	if len(recs2) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs2))
	}
	if recs2[0].Version != 1 {
		t.Errorf("first record Version = %d, want 1 (ascending)", recs2[0].Version)
	}

	// Offset 3, ascending.
	recs3, err := s.ListVersions(context.Background(), "user", "u1",
		port.WithOrderAsc(), port.WithOffset(3))
	if err != nil {
		t.Fatalf("ListVersions(offset 3) error: %v", err)
	}
	if len(recs3) != 2 {
		t.Fatalf("expected 2 records after offset 3, got %d", len(recs3))
	}
	if recs3[0].Version != 4 {
		t.Errorf("first record Version = %d, want 4", recs3[0].Version)
	}
}

func TestInMemoryStore_SaveInTx(t *testing.T) {
	s := NewInMemoryStore()

	err := s.SaveInTx(context.Background(), nil, domain.VersionRecord{
		EntityType: "user", EntityID: "u1", Version: 1,
	})
	if err != nil {
		t.Fatalf("SaveInTx() error: %v", err)
	}
	if s.Len() != 1 {
		t.Errorf("Len() = %d, want 1", s.Len())
	}
}

func TestInMemoryStore_Reset(t *testing.T) {
	s := NewInMemoryStore()
	_ = s.Save(context.Background(), domain.VersionRecord{
		EntityType: "user", EntityID: "u1", Version: 1,
	})
	s.Reset()
	if s.Len() != 0 {
		t.Errorf("Len() after Reset = %d, want 0", s.Len())
	}
}

func TestInMemoryStore_RecordsFor(t *testing.T) {
	s := NewInMemoryStore()
	_ = s.Save(context.Background(), domain.VersionRecord{
		EntityType: "user", EntityID: "u1", Version: 1,
	})
	_ = s.Save(context.Background(), domain.VersionRecord{
		EntityType: "order", EntityID: "o1", Version: 1,
	})

	recs := s.RecordsFor("user", "u1")
	if len(recs) != 1 {
		t.Errorf("RecordsFor = %d records, want 1", len(recs))
	}
}
