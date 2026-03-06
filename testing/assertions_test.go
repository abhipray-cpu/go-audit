package audittest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

func TestAssertVersionCount(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()

	for i := int64(1); i <= 3; i++ {
		_ = s.Save(ctx, domain.VersionRecord{
			EntityType: "user", EntityID: "u1", Version: i, CreatedAt: time.Now(),
		})
	}

	// Should pass (3 versions).
	AssertVersionCount(t, s, "user", "u1", 3)
}

func TestAssertFieldChanged(t *testing.T) {
	rec := domain.VersionRecord{
		Version: 2,
		Delta: &domain.Delta{
			Changes: []domain.FieldChange{
				{Path: "Name", OldValue: json.RawMessage(`"Alice"`), NewValue: json.RawMessage(`"Bob"`)},
			},
		},
	}

	AssertFieldChanged(t, rec, "Name")
}

func TestAssertActorIs(t *testing.T) {
	rec := domain.VersionRecord{
		Version:  1,
		Metadata: domain.VersionMetadata{ActorID: "user-42"},
	}

	AssertActorIs(t, rec, "user-42")
}

func TestAssertLatestVersion(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()

	for i := int64(1); i <= 5; i++ {
		_ = s.Save(ctx, domain.VersionRecord{
			EntityType: "user", EntityID: "u1", Version: i, CreatedAt: time.Now(),
		})
	}

	AssertLatestVersion(t, s, "user", "u1", 5)
}

func TestAssertVersionCount_WithListOptions(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()

	_ = s.Save(ctx, domain.VersionRecord{
		EntityType: "order", EntityID: "o1", Version: 1, CreatedAt: time.Now(),
	})

	// Verify the assertion works with port.ListOption being applied internally.
	recs, err := s.ListVersions(ctx, "order", "o1", port.WithLimit(10))
	if err != nil {
		t.Fatalf("ListVersions error: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
}
