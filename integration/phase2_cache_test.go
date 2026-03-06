//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/abhipray-cpu/go-audit/adapter/cache"
	"github.com/abhipray-cpu/go-audit/domain"
)

// ---------------------------------------------------------------------------
// Phase 2 — CA-001 to CA-005: LRU Cache
// ---------------------------------------------------------------------------

// CA-001: TestCache_PutAndGet verifies Put then Get returns the record.
func TestCache_PutAndGet(t *testing.T) {
	c := cache.New(10)
	ctx := context.Background()

	rec := domain.VersionRecord{
		EntityType: "user",
		EntityID:   "u1",
		Version:    1,
		Data:       []byte(`{"name":"Alice"}`),
	}

	c.Put(ctx, rec)
	got, ok := c.Get(ctx, "user", "u1", 1)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if string(got.Data) != string(rec.Data) {
		t.Fatalf("data mismatch: %s vs %s", got.Data, rec.Data)
	}
}

// CA-002: TestCache_Miss verifies Get returns false for absent key.
func TestCache_Miss(t *testing.T) {
	c := cache.New(10)
	ctx := context.Background()

	_, ok := c.Get(ctx, "user", "u1", 999)
	if ok {
		t.Fatal("expected cache miss")
	}
}

// CA-003: TestCache_Eviction verifies LRU eviction when capacity exceeded.
func TestCache_Eviction(t *testing.T) {
	c := cache.New(3) // capacity of 3
	ctx := context.Background()

	for i := int64(1); i <= 4; i++ {
		c.Put(ctx, domain.VersionRecord{
			EntityType: "user",
			EntityID:   "u1",
			Version:    i,
			Data:       []byte(`{}`),
		})
	}

	// First entry (v1) should have been evicted.
	_, ok := c.Get(ctx, "user", "u1", 1)
	if ok {
		t.Fatal("expected v1 to be evicted")
	}

	// Latest entries should still be present.
	_, ok = c.Get(ctx, "user", "u1", 4)
	if !ok {
		t.Fatal("expected v4 to be present")
	}
}

// CA-004: TestCache_Invalidate verifies Invalidate removes all versions of an entity.
func TestCache_Invalidate(t *testing.T) {
	c := cache.New(10)
	ctx := context.Background()

	for i := int64(1); i <= 3; i++ {
		c.Put(ctx, domain.VersionRecord{
			EntityType: "user",
			EntityID:   "u1",
			Version:    i,
			Data:       []byte(`{}`),
		})
	}

	c.Invalidate(ctx, "user", "u1")

	for i := int64(1); i <= 3; i++ {
		_, ok := c.Get(ctx, "user", "u1", i)
		if ok {
			t.Fatalf("expected v%d to be invalidated", i)
		}
	}
}

// CA-005: TestCache_Len verifies the Len method.
func TestCache_Len(t *testing.T) {
	c := cache.New(10)
	ctx := context.Background()

	if c.Len() != 0 {
		t.Fatalf("expected 0, got %d", c.Len())
	}

	c.Put(ctx, domain.VersionRecord{EntityType: "user", EntityID: "u1", Version: 1})
	c.Put(ctx, domain.VersionRecord{EntityType: "user", EntityID: "u1", Version: 2})

	if c.Len() != 2 {
		t.Fatalf("expected 2, got %d", c.Len())
	}
}
