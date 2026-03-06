package app

import (
	"context"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
)

func TestRetention_ExpiredRecordsDeleted(t *testing.T) {
	now := time.Now()
	records := []domain.VersionRecord{
		{EntityType: "user", EntityID: "u1", Version: 1, CreatedAt: now.AddDate(0, 0, -100)}, // expired (>30 days)
		{EntityType: "user", EntityID: "u1", Version: 2, CreatedAt: now.AddDate(0, 0, -50)},  // expired
		{EntityType: "user", EntityID: "u1", Version: 3, CreatedAt: now.AddDate(0, 0, -10)},  // kept
	}

	deleter := NewMemoryDeleter(&records)
	engine := &RetentionEngine{
		cfg: RetentionConfig{
			RetentionDays: map[string]int{"user": 30},
			Deleter:       deleter,
		},
	}

	results := engine.RunOnce(context.Background())

	if results["user"] != 2 {
		t.Fatalf("expected 2 deleted, got %d", results["user"])
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 remaining, got %d", len(records))
	}
	if records[0].Version != 3 {
		t.Fatalf("expected version 3 to survive, got %d", records[0].Version)
	}
}

func TestRetention_PerEntity(t *testing.T) {
	now := time.Now()
	records := []domain.VersionRecord{
		{EntityType: "user", EntityID: "u1", Version: 1, CreatedAt: now.AddDate(0, 0, -100)},
		{EntityType: "order", EntityID: "o1", Version: 1, CreatedAt: now.AddDate(0, 0, -100)},
		{EntityType: "user", EntityID: "u1", Version: 2, CreatedAt: now.AddDate(0, 0, -5)},
		{EntityType: "order", EntityID: "o1", Version: 2, CreatedAt: now.AddDate(0, 0, -5)},
	}

	deleter := NewMemoryDeleter(&records)
	engine := &RetentionEngine{
		cfg: RetentionConfig{
			RetentionDays: map[string]int{
				"user":  30,  // 30-day retention → user v1 deleted
				"order": 365, // 365-day retention → order v1 kept (only 100 days old)
			},
			Deleter: deleter,
		},
	}

	results := engine.RunOnce(context.Background())

	if results["user"] != 1 {
		t.Fatalf("expected 1 user deleted, got %d", results["user"])
	}
	if results["order"] != 0 {
		t.Fatalf("expected 0 orders deleted, got %d", results["order"])
	}

	// Remaining: user v2, order v1, order v2
	if len(records) != 3 {
		t.Fatalf("expected 3 remaining, got %d", len(records))
	}
}

func TestRetention_BackgroundGoroutine(t *testing.T) {
	records := []domain.VersionRecord{
		{EntityType: "user", EntityID: "u1", Version: 1, CreatedAt: time.Now().AddDate(0, 0, -100)},
	}

	deleter := NewMemoryDeleter(&records)
	engine := NewRetentionEngine(RetentionConfig{
		RetentionDays: map[string]int{"user": 30},
		Deleter:       deleter,
		Interval:      50 * time.Millisecond, // fast tick for test
	})
	defer engine.Stop()

	// Wait for at least one cleanup cycle.
	time.Sleep(200 * time.Millisecond)

	// Use the deleter's mutex to safely read the records slice.
	deleter.mu.Lock()
	remaining := len(records)
	deleter.mu.Unlock()

	if remaining != 0 {
		t.Fatalf("expected all expired records deleted by background goroutine, got %d remaining", remaining)
	}
}
