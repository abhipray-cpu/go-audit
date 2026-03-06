package noop

import (
	"context"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

func TestNoopWAL_ZeroOverhead(t *testing.T) {
	w := New()

	entry := port.WALEntry{
		ID:        "e1",
		Record:    domain.VersionRecord{ID: "r1", EntityType: "user", EntityID: "u1", Version: 1},
		CreatedAt: time.Now(),
	}

	// Append returns nil.
	if err := w.Append(context.Background(), entry); err != nil {
		t.Fatalf("Append() = %v, want nil", err)
	}

	// Ack returns nil.
	if err := w.Ack(context.Background(), entry.ID); err != nil {
		t.Fatalf("Ack() = %v, want nil", err)
	}

	// Replay returns empty.
	entries, err := w.Replay(context.Background())
	if err != nil {
		t.Fatalf("Replay() error = %v, want nil", err)
	}
	if len(entries) != 0 {
		t.Errorf("Replay() returned %d entries, want 0", len(entries))
	}
}

func BenchmarkNoopWAL_Append(b *testing.B) {
	w := New()
	entry := port.WALEntry{
		ID:        "e1",
		Record:    domain.VersionRecord{ID: "r1"},
		CreatedAt: time.Now(),
	}
	ctx := context.Background()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Append(ctx, entry)
	}
}
