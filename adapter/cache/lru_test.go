package cache

import (
	"context"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain"
)

func makeRecord(entityType, entityID string, version int64) domain.VersionRecord {
	return domain.VersionRecord{
		ID:         "rec",
		EntityType: entityType,
		EntityID:   entityID,
		Version:    version,
		Data:       []byte(`{"id":"` + entityID + `"}`),
	}
}

func TestLRUCache_HitAfterPut(t *testing.T) {
	c := New(10)
	ctx := context.Background()

	rec := makeRecord("user", "u1", 1)
	c.Put(ctx, rec)

	got, ok := c.Get(ctx, "user", "u1", 1)
	if !ok {
		t.Fatal("expected cache hit, got miss")
	}
	if got.EntityID != "u1" || got.Version != 1 {
		t.Errorf("got record = {EntityID:%q, Version:%d}, want {u1, 1}", got.EntityID, got.Version)
	}
}

func TestLRUCache_Miss(t *testing.T) {
	c := New(10)
	ctx := context.Background()

	_, ok := c.Get(ctx, "user", "u1", 1)
	if ok {
		t.Error("expected cache miss, got hit")
	}
}

func TestLRUCache_Eviction(t *testing.T) {
	c := New(3) // capacity 3
	ctx := context.Background()

	// Put 4 entries → oldest (v1) should be evicted.
	c.Put(ctx, makeRecord("user", "u1", 1))
	c.Put(ctx, makeRecord("user", "u1", 2))
	c.Put(ctx, makeRecord("user", "u1", 3))
	c.Put(ctx, makeRecord("user", "u1", 4))

	// v1 should be evicted.
	if _, ok := c.Get(ctx, "user", "u1", 1); ok {
		t.Error("expected v1 to be evicted, but got cache hit")
	}

	// v2, v3, v4 should still be present.
	for _, v := range []int64{2, 3, 4} {
		if _, ok := c.Get(ctx, "user", "u1", v); !ok {
			t.Errorf("expected v%d cache hit, got miss", v)
		}
	}

	if c.Len() != 3 {
		t.Errorf("Len() = %d, want 3", c.Len())
	}
}

func TestLRUCache_WriteThrough(t *testing.T) {
	c := New(10)
	ctx := context.Background()

	// Put initial record.
	rec := makeRecord("user", "u1", 1)
	rec.Data = []byte(`{"name":"Alice"}`)
	c.Put(ctx, rec)

	// Update same key (write-through).
	rec.Data = []byte(`{"name":"Bob"}`)
	c.Put(ctx, rec)

	got, ok := c.Get(ctx, "user", "u1", 1)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if string(got.Data) != `{"name":"Bob"}` {
		t.Errorf("Data = %s, want updated value", got.Data)
	}
}

func TestLRUCache_Invalidate(t *testing.T) {
	c := New(10)
	ctx := context.Background()

	c.Put(ctx, makeRecord("user", "u1", 1))
	c.Put(ctx, makeRecord("user", "u1", 2))
	c.Put(ctx, makeRecord("user", "u2", 1))

	// Invalidate u1 → only u2 remains.
	c.Invalidate(ctx, "user", "u1")

	if _, ok := c.Get(ctx, "user", "u1", 1); ok {
		t.Error("expected u1:v1 to be invalidated")
	}
	if _, ok := c.Get(ctx, "user", "u1", 2); ok {
		t.Error("expected u1:v2 to be invalidated")
	}
	if _, ok := c.Get(ctx, "user", "u2", 1); !ok {
		t.Error("expected u2:v1 to still be present")
	}
}

func TestLRUCache_LRUOrdering(t *testing.T) {
	c := New(3)
	ctx := context.Background()

	c.Put(ctx, makeRecord("user", "u1", 1))
	c.Put(ctx, makeRecord("user", "u1", 2))
	c.Put(ctx, makeRecord("user", "u1", 3))

	// Access v1 to promote it to MRU.
	c.Get(ctx, "user", "u1", 1)

	// Add v4 → v2 should be evicted (it's now LRU).
	c.Put(ctx, makeRecord("user", "u1", 4))

	if _, ok := c.Get(ctx, "user", "u1", 2); ok {
		t.Error("expected v2 to be evicted (LRU after v1 was promoted)")
	}
	// v1, v3, v4 should remain.
	for _, v := range []int64{1, 3, 4} {
		if _, ok := c.Get(ctx, "user", "u1", v); !ok {
			t.Errorf("expected v%d cache hit", v)
		}
	}
}

func BenchmarkLRUCache_Put(b *testing.B) {
	c := New(10000)
	ctx := context.Background()
	rec := makeRecord("user", "u1", 1)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec.Version = int64(i)
		c.Put(ctx, rec)
	}
}

func BenchmarkLRUCache_Get(b *testing.B) {
	c := New(10000)
	ctx := context.Background()

	// Pre-fill.
	for i := 0; i < 10000; i++ {
		c.Put(ctx, makeRecord("user", "u1", int64(i)))
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.Get(ctx, "user", "u1", int64(i%10000))
	}
}
