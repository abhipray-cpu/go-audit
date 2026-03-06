//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain/diff"
)

// ---------------------------------------------------------------------------
// Phase 1 — D-050 to D-053: time.Time diff tests
// ---------------------------------------------------------------------------

// D-050: TestDiff_Time_Change verifies time.Time field change detection.
func TestDiff_Time_Change(t *testing.T) {
	e := diff.New()
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	prev := &TimeEntity{ID: "1", CreatedAt: now}
	curr := &TimeEntity{ID: "1", CreatedAt: now.Add(1 * time.Hour)}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(delta.Changes), delta.Changes)
	}
	if delta.Changes[0].Path != "CreatedAt" {
		t.Errorf("expected path CreatedAt, got %s", delta.Changes[0].Path)
	}
}

// D-051: TestDiff_Time_SameInstantDifferentTZ verifies timezone-independent comparison.
func TestDiff_Time_SameInstantDifferentTZ(t *testing.T) {
	e := diff.New()
	utc := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	ist := utc.In(time.FixedZone("IST", 5*60*60+30*60))

	prev := &TimeEntity{ID: "1", CreatedAt: utc}
	curr := &TimeEntity{ID: "1", CreatedAt: ist}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !delta.IsEmpty() {
		t.Fatalf("expected empty delta (same instant different TZ), got %d changes: %+v",
			len(delta.Changes), delta.Changes)
	}
}

// D-052: TestDiff_Time_ZeroToValue verifies zero time → real time.
func TestDiff_Time_ZeroToValue(t *testing.T) {
	e := diff.New()
	prev := &TimeEntity{ID: "1", CreatedAt: time.Time{}}
	curr := &TimeEntity{ID: "1", CreatedAt: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
}

// D-053: TestDiff_Time_ValueToZero verifies real time → zero time.
func TestDiff_Time_ValueToZero(t *testing.T) {
	e := diff.New()
	prev := &TimeEntity{ID: "1", CreatedAt: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)}
	curr := &TimeEntity{ID: "1", CreatedAt: time.Time{}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
}
