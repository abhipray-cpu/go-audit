//go:build integration

package integration

import (
	"context"
	"math"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain/diff"
)

// ---------------------------------------------------------------------------
// Phase 1 — D-001 to D-010: Flat entity primitive diff tests
// ---------------------------------------------------------------------------

func newEngine() *diff.Engine { return diff.New() }

// D-001: TestDiff_Flat_StringChange verifies a single string field change is detected.
func TestDiff_Flat_StringChange(t *testing.T) {
	e := newEngine()
	prev := &FlatEntity{ID: "1", Name: "Alice", Age: 25, Score: 95.5, Active: true}
	curr := &FlatEntity{ID: "1", Name: "Bob", Age: 25, Score: 95.5, Active: true}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(delta.Changes), delta.Changes)
	}
	if delta.Changes[0].Path != "Name" {
		t.Errorf("expected path Name, got %s", delta.Changes[0].Path)
	}
}

// D-002: TestDiff_Flat_IntChange verifies a single int field change is detected.
func TestDiff_Flat_IntChange(t *testing.T) {
	e := newEngine()
	prev := &FlatEntity{ID: "1", Name: "Alice", Age: 25}
	curr := &FlatEntity{ID: "1", Name: "Alice", Age: 30}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
	if delta.Changes[0].Path != "Age" {
		t.Errorf("expected path Age, got %s", delta.Changes[0].Path)
	}
}

// D-003: TestDiff_Flat_FloatChange verifies a float64 field change is detected.
func TestDiff_Flat_FloatChange(t *testing.T) {
	e := newEngine()
	prev := &FlatEntity{ID: "1", Score: 95.5}
	curr := &FlatEntity{ID: "1", Score: 97.2}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
	if delta.Changes[0].Path != "Score" {
		t.Errorf("expected path Score, got %s", delta.Changes[0].Path)
	}
}

// D-004: TestDiff_Flat_BoolChange verifies a bool field change is detected.
func TestDiff_Flat_BoolChange(t *testing.T) {
	e := newEngine()
	prev := &FlatEntity{ID: "1", Active: true}
	curr := &FlatEntity{ID: "1", Active: false}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
	if delta.Changes[0].Path != "Active" {
		t.Errorf("expected path Active, got %s", delta.Changes[0].Path)
	}
}

// D-005: TestDiff_Flat_NoChange verifies that identical entities produce an empty delta.
func TestDiff_Flat_NoChange(t *testing.T) {
	e := newEngine()
	prev := &FlatEntity{ID: "1", Name: "Alice", Age: 25, Score: 95.5, Active: true}
	curr := &FlatEntity{ID: "1", Name: "Alice", Age: 25, Score: 95.5, Active: true}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !delta.IsEmpty() {
		t.Fatalf("expected empty delta, got %d changes: %+v", len(delta.Changes), delta.Changes)
	}
}

// D-006: TestDiff_Flat_AllFieldsChange verifies all tracked fields change simultaneously.
func TestDiff_Flat_AllFieldsChange(t *testing.T) {
	e := newEngine()
	prev := &FlatEntity{ID: "1", Name: "Alice", Age: 25, Score: 95.5, Active: true}
	curr := &FlatEntity{ID: "1", Name: "Bob", Age: 30, Score: 97.2, Active: false}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 4 {
		t.Fatalf("expected 4 changes, got %d: %+v", len(delta.Changes), delta.Changes)
	}
	paths := make(map[string]bool)
	for _, c := range delta.Changes {
		paths[c.Path] = true
	}
	for _, p := range []string{"Name", "Age", "Score", "Active"} {
		if !paths[p] {
			t.Errorf("missing change for path %s", p)
		}
	}
}

// D-007: TestDiff_Flat_EmptyStringToValue verifies transition from "" to "Alice".
func TestDiff_Flat_EmptyStringToValue(t *testing.T) {
	e := newEngine()
	prev := &FlatEntity{ID: "1", Name: ""}
	curr := &FlatEntity{ID: "1", Name: "Alice"}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
}

// D-008: TestDiff_Flat_ValueToEmptyString verifies transition from "Alice" to "".
func TestDiff_Flat_ValueToEmptyString(t *testing.T) {
	e := newEngine()
	prev := &FlatEntity{ID: "1", Name: "Alice"}
	curr := &FlatEntity{ID: "1", Name: ""}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
}

// D-009: TestDiff_Flat_ZeroIntToValue verifies zero-value to non-zero int.
func TestDiff_Flat_ZeroIntToValue(t *testing.T) {
	e := newEngine()
	prev := &FlatEntity{ID: "1", Age: 0}
	curr := &FlatEntity{ID: "1", Age: 25}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
}

// D-010: TestDiff_Flat_LargeNumbers verifies extreme float64 values.
func TestDiff_Flat_LargeNumbers(t *testing.T) {
	e := newEngine()
	prev := &FlatEntity{ID: "1", Score: 0}
	curr := &FlatEntity{ID: "1", Score: math.MaxFloat64}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
}
