//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain/diff"
)

// ---------------------------------------------------------------------------
// Phase 1 — D-057 to D-060: Version tag behavior tests
// ---------------------------------------------------------------------------

// D-057: TestDiff_Tags_IgnoredFieldExcluded verifies version:"ignore" fields never appear in delta.
func TestDiff_Tags_IgnoredFieldExcluded(t *testing.T) {
	e := diff.New()
	prev := &TaggedEntity{ID: "1", Tracked1: "a", Ignored: "old"}
	curr := &TaggedEntity{ID: "1", Tracked1: "a", Ignored: "new"}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !delta.IsEmpty() {
		t.Fatalf("expected empty delta (ignored field changed), got %d changes: %+v",
			len(delta.Changes), delta.Changes)
	}
}

// D-058: TestDiff_Tags_OnlyTrackedInDelta verifies untagged fields are excluded in tracked mode.
func TestDiff_Tags_OnlyTrackedInDelta(t *testing.T) {
	e := diff.New()
	// Normal has no tag — since other fields are tracked, Normal is excluded.
	prev := &TaggedEntity{ID: "1", Tracked1: "a", Normal: "old"}
	curr := &TaggedEntity{ID: "1", Tracked1: "b", Normal: "new"}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only Tracked1 should appear; Normal is excluded because tracked mode is active.
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change (only tracked), got %d: %+v", len(delta.Changes), delta.Changes)
	}
	if delta.Changes[0].Path != "Tracked1" {
		t.Errorf("expected Tracked1, got %s", delta.Changes[0].Path)
	}
}

// D-059: TestDiff_Tags_IDNeverInDelta verifies the ID field never appears in delta.
func TestDiff_Tags_IDNeverInDelta(t *testing.T) {
	e := diff.New()
	// Changing ID between prev and curr — but ID is tagged version:"id" → excluded.
	prev := &TaggedEntity{ID: "1", Tracked1: "a"}
	curr := &TaggedEntity{ID: "2", Tracked1: "a"}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range delta.Changes {
		if c.Path == "ID" {
			t.Errorf("ID field should never appear in delta, but found it")
		}
	}
}

// D-060: TestDiff_Tags_RedactableInDelta verifies redactable fields still appear in delta.
func TestDiff_Tags_RedactableInDelta(t *testing.T) {
	e := diff.New()
	prev := &TaggedEntity{ID: "1", Redactable: "secret1"}
	curr := &TaggedEntity{ID: "1", Redactable: "secret2"}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
	if delta.Changes[0].Path != "Redactable" {
		t.Errorf("expected Redactable, got %s", delta.Changes[0].Path)
	}
}
