//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain/diff"
)

// ---------------------------------------------------------------------------
// Phase 1 — D-019 to D-030: Map diff tests
// ---------------------------------------------------------------------------

// D-019: TestDiff_Map_AddKey verifies detection of a new map key.
func TestDiff_Map_AddKey(t *testing.T) {
	e := diff.New()
	prev := &MapEntity{ID: "1", Tags: map[string]string{}}
	curr := &MapEntity{ID: "1", Tags: map[string]string{"role": "admin"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(delta.Changes), delta.Changes)
	}
}

// D-020: TestDiff_Map_RemoveKey verifies detection of a removed map key.
func TestDiff_Map_RemoveKey(t *testing.T) {
	e := diff.New()
	prev := &MapEntity{ID: "1", Tags: map[string]string{"role": "admin"}}
	curr := &MapEntity{ID: "1", Tags: map[string]string{}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(delta.Changes), delta.Changes)
	}
}

// D-021: TestDiff_Map_ChangeValue verifies detection of a changed map value.
func TestDiff_Map_ChangeValue(t *testing.T) {
	e := diff.New()
	prev := &MapEntity{ID: "1", Tags: map[string]string{"role": "admin"}}
	curr := &MapEntity{ID: "1", Tags: map[string]string{"role": "user"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(delta.Changes), delta.Changes)
	}
	if delta.Changes[0].Path != "Tags.role" {
		t.Errorf("expected Tags.role, got %s", delta.Changes[0].Path)
	}
}

// D-022: TestDiff_Map_MultipleOps verifies simultaneous add, remove, and change.
func TestDiff_Map_MultipleOps(t *testing.T) {
	e := diff.New()
	prev := &MapEntity{
		ID:   "1",
		Tags: map[string]string{"role": "admin", "dept": "eng", "keep": "same"},
	}
	curr := &MapEntity{
		ID:   "1",
		Tags: map[string]string{"role": "user", "keep": "same", "new": "val"},
	}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// role changed, dept removed, new added → 3 changes
	if len(delta.Changes) != 3 {
		t.Fatalf("expected 3 changes, got %d: %+v", len(delta.Changes), delta.Changes)
	}
}

// D-023: TestDiff_Map_NoChange verifies identical maps produce empty delta.
func TestDiff_Map_NoChange(t *testing.T) {
	e := diff.New()
	prev := &MapEntity{ID: "1", Tags: map[string]string{"a": "1", "b": "2"}}
	curr := &MapEntity{ID: "1", Tags: map[string]string{"a": "1", "b": "2"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !delta.IsEmpty() {
		t.Fatalf("expected empty delta, got %d changes", len(delta.Changes))
	}
}

// D-024: TestDiff_Map_NilToEmpty verifies nil → empty map is treated as no change.
func TestDiff_Map_NilToEmpty(t *testing.T) {
	e := diff.New()
	prev := &MapEntity{ID: "1", Tags: nil}
	curr := &MapEntity{ID: "1", Tags: map[string]string{}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !delta.IsEmpty() {
		t.Fatalf("expected empty delta (nil vs empty map), got %d changes: %+v", len(delta.Changes), delta.Changes)
	}
}

// D-025: TestDiff_Map_EmptyToNil verifies empty map → nil is treated as no change.
func TestDiff_Map_EmptyToNil(t *testing.T) {
	e := diff.New()
	prev := &MapEntity{ID: "1", Tags: map[string]string{}}
	curr := &MapEntity{ID: "1", Tags: nil}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !delta.IsEmpty() {
		t.Fatalf("expected empty delta, got %d changes: %+v", len(delta.Changes), delta.Changes)
	}
}

// D-026: TestDiff_Map_NilToPopulated verifies nil → populated map detects change.
func TestDiff_Map_NilToPopulated(t *testing.T) {
	e := diff.New()
	prev := &MapEntity{ID: "1", Tags: nil}
	curr := &MapEntity{ID: "1", Tags: map[string]string{"a": "1"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(delta.Changes), delta.Changes)
	}
}

// D-027: TestDiff_Map_NestedMap verifies change detection in nested maps.
func TestDiff_Map_NestedMap(t *testing.T) {
	e := diff.New()
	prev := &MapEntity{
		ID: "1",
		Nested: map[string]map[string]string{
			"group1": {"key1": "val1"},
		},
	}
	curr := &MapEntity{
		ID: "1",
		Nested: map[string]map[string]string{
			"group1": {"key1": "val2"},
		},
	}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) < 1 {
		t.Fatalf("expected at least 1 change, got %d", len(delta.Changes))
	}
}

// D-028: TestDiff_Map_NestedAddGroup verifies adding a new group in nested maps.
func TestDiff_Map_NestedAddGroup(t *testing.T) {
	e := diff.New()
	prev := &MapEntity{
		ID:     "1",
		Nested: map[string]map[string]string{},
	}
	curr := &MapEntity{
		ID: "1",
		Nested: map[string]map[string]string{
			"group1": {"k": "v"},
		},
	}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) < 1 {
		t.Fatalf("expected at least 1 change, got %d", len(delta.Changes))
	}
}

// D-029: TestDiff_Map_AnyValues verifies diff of map[string]any with numeric values.
func TestDiff_Map_AnyValues(t *testing.T) {
	e := diff.New()
	prev := &MapEntity{
		ID:       "1",
		Settings: map[string]any{"count": 1},
	}
	curr := &MapEntity{
		ID:       "1",
		Settings: map[string]any{"count": 2},
	}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(delta.Changes), delta.Changes)
	}
}

// D-030: TestDiff_Map_MixedAnyTypes verifies diff detects type change within map[string]any.
func TestDiff_Map_MixedAnyTypes(t *testing.T) {
	e := diff.New()
	prev := &MapEntity{
		ID:       "1",
		Settings: map[string]any{"val": "str"},
	}
	curr := &MapEntity{
		ID:       "1",
		Settings: map[string]any{"val": 42},
	}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(delta.Changes), delta.Changes)
	}
}
