//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain/diff"
)

// ---------------------------------------------------------------------------
// Phase 1 — D-031 to D-040: Slice diff tests
// ---------------------------------------------------------------------------

// D-031: TestDiff_Slice_AppendElement verifies appending an element is detected.
func TestDiff_Slice_AppendElement(t *testing.T) {
	e := diff.New()
	prev := &SliceEntity{ID: "1", Roles: []string{"a"}}
	curr := &SliceEntity{ID: "1", Roles: []string{"a", "b"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(delta.Changes), delta.Changes)
	}
	if delta.Changes[0].Path != "Roles" {
		t.Errorf("expected path Roles, got %s", delta.Changes[0].Path)
	}
}

// D-032: TestDiff_Slice_RemoveElement verifies removing an element is detected.
func TestDiff_Slice_RemoveElement(t *testing.T) {
	e := diff.New()
	prev := &SliceEntity{ID: "1", Roles: []string{"a", "b"}}
	curr := &SliceEntity{ID: "1", Roles: []string{"a"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
}

// D-033: TestDiff_Slice_ReorderElements verifies reordering is detected for non-normalized slices.
func TestDiff_Slice_ReorderElements(t *testing.T) {
	e := diff.New()
	prev := &SliceEntity{ID: "1", Roles: []string{"a", "b"}}
	curr := &SliceEntity{ID: "1", Roles: []string{"b", "a"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Reorder in a non-normalized slice should be detected as a change.
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change (reorder matters), got %d: %+v", len(delta.Changes), delta.Changes)
	}
}

// D-034: TestDiff_Slice_NilToPopulated verifies nil → populated slice.
func TestDiff_Slice_NilToPopulated(t *testing.T) {
	e := diff.New()
	prev := &SliceEntity{ID: "1", Roles: nil}
	curr := &SliceEntity{ID: "1", Roles: []string{"a"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
}

// D-035: TestDiff_Slice_PopulatedToNil verifies populated → nil slice.
func TestDiff_Slice_PopulatedToNil(t *testing.T) {
	e := diff.New()
	prev := &SliceEntity{ID: "1", Roles: []string{"a"}}
	curr := &SliceEntity{ID: "1", Roles: nil}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
}

// D-036: TestDiff_Slice_EmptyToPopulated verifies [] → ["a"].
func TestDiff_Slice_EmptyToPopulated(t *testing.T) {
	e := diff.New()
	prev := &SliceEntity{ID: "1", Roles: []string{}}
	curr := &SliceEntity{ID: "1", Roles: []string{"a"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
}

// D-037: TestDiff_Slice_NoChange verifies identical slices produce empty delta.
func TestDiff_Slice_NoChange(t *testing.T) {
	e := diff.New()
	prev := &SliceEntity{ID: "1", Roles: []string{"a", "b"}}
	curr := &SliceEntity{ID: "1", Roles: []string{"a", "b"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !delta.IsEmpty() {
		t.Fatalf("expected empty delta, got %d changes", len(delta.Changes))
	}
}

// D-038: TestDiff_Slice_NormalizedIgnoresOrder verifies normalized tag ignores order.
func TestDiff_Slice_NormalizedIgnoresOrder(t *testing.T) {
	e := diff.New()
	prev := &SliceEntity{ID: "1", SortedTags: []string{"b", "a"}}
	curr := &SliceEntity{ID: "1", SortedTags: []string{"a", "b"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !delta.IsEmpty() {
		t.Fatalf("expected empty delta (normalized ignores order), got %d changes: %+v", len(delta.Changes), delta.Changes)
	}
}

// D-039: TestDiff_Slice_NormalizedDetectsContent verifies normalized still detects content changes.
func TestDiff_Slice_NormalizedDetectsContent(t *testing.T) {
	e := diff.New()
	prev := &SliceEntity{ID: "1", SortedTags: []string{"a", "b"}}
	curr := &SliceEntity{ID: "1", SortedTags: []string{"a", "c"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
}

// D-040: TestDiff_Slice_IntSliceChange verifies int slice content change.
func TestDiff_Slice_IntSliceChange(t *testing.T) {
	e := diff.New()
	prev := &SliceEntity{ID: "1", Scores: []int{1, 2, 3}}
	curr := &SliceEntity{ID: "1", Scores: []int{1, 2, 4}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
}
