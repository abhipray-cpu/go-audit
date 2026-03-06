//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain/diff"
)

// ---------------------------------------------------------------------------
// Phase 1 — D-041 to D-049: Pointer + nil transition diff tests
// ---------------------------------------------------------------------------

// D-041: TestDiff_Ptr_NilToValue verifies nil → *string transition.
func TestDiff_Ptr_NilToValue(t *testing.T) {
	e := diff.New()
	prev := &PointerEntity{ID: "1", Nickname: nil}
	curr := &PointerEntity{ID: "1", Nickname: strPtr("Bob")}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(delta.Changes), delta.Changes)
	}
	if delta.Changes[0].Path != "Nickname" {
		t.Errorf("expected path Nickname, got %s", delta.Changes[0].Path)
	}
	// OldValue should be nil (nil pointer represented as nil json.RawMessage)
	if delta.Changes[0].OldValue != nil {
		t.Errorf("expected OldValue nil for nil→value pointer, got %s", delta.Changes[0].OldValue)
	}
}

// D-042: TestDiff_Ptr_ValueToNil verifies *string → nil transition.
func TestDiff_Ptr_ValueToNil(t *testing.T) {
	e := diff.New()
	prev := &PointerEntity{ID: "1", Nickname: strPtr("Bob")}
	curr := &PointerEntity{ID: "1", Nickname: nil}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
	// NewValue should be nil (value→nil pointer represented as nil json.RawMessage)
	if delta.Changes[0].NewValue != nil {
		t.Errorf("expected NewValue nil for value→nil pointer, got %s", delta.Changes[0].NewValue)
	}
}

// D-043: TestDiff_Ptr_ValueToValue verifies *string value change.
func TestDiff_Ptr_ValueToValue(t *testing.T) {
	e := diff.New()
	prev := &PointerEntity{ID: "1", Nickname: strPtr("Bob")}
	curr := &PointerEntity{ID: "1", Nickname: strPtr("Alice")}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
}

// D-044: TestDiff_Ptr_BothNil verifies nil → nil produces empty delta.
func TestDiff_Ptr_BothNil(t *testing.T) {
	e := diff.New()
	prev := &PointerEntity{ID: "1", Nickname: nil}
	curr := &PointerEntity{ID: "1", Nickname: nil}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !delta.IsEmpty() {
		t.Fatalf("expected empty delta, got %d changes", len(delta.Changes))
	}
}

// D-045: TestDiff_Ptr_StructNilToValue verifies nil → *Address transition.
func TestDiff_Ptr_StructNilToValue(t *testing.T) {
	e := diff.New()
	prev := &PointerEntity{ID: "1", Address: nil}
	curr := &PointerEntity{ID: "1", Address: &Address{City: "NYC"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) < 1 {
		t.Fatalf("expected at least 1 change, got %d", len(delta.Changes))
	}
}

// D-046: TestDiff_Ptr_StructValueToNil verifies *Address → nil transition.
func TestDiff_Ptr_StructValueToNil(t *testing.T) {
	e := diff.New()
	prev := &PointerEntity{ID: "1", Address: &Address{City: "NYC"}}
	curr := &PointerEntity{ID: "1", Address: nil}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) < 1 {
		t.Fatalf("expected at least 1 change, got %d", len(delta.Changes))
	}
}

// D-047: TestDiff_Ptr_StructLeafChange verifies change through non-nil pointer-to-struct.
func TestDiff_Ptr_StructLeafChange(t *testing.T) {
	e := diff.New()
	prev := &PointerEntity{ID: "1", Address: &Address{City: "NYC", Street: "1st"}}
	curr := &PointerEntity{ID: "1", Address: &Address{City: "LA", Street: "1st"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(delta.Changes), delta.Changes)
	}
	if delta.Changes[0].Path != "Address.City" {
		t.Errorf("expected Address.City, got %s", delta.Changes[0].Path)
	}
}

// D-048: TestDiff_Ptr_IntNilToValue verifies nil → *int transition.
func TestDiff_Ptr_IntNilToValue(t *testing.T) {
	e := diff.New()
	prev := &PointerEntity{ID: "1", Score: nil}
	curr := &PointerEntity{ID: "1", Score: intPtr(42)}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
}

// D-049: TestDiff_Ptr_IntValueChange verifies *int value change.
func TestDiff_Ptr_IntValueChange(t *testing.T) {
	e := diff.New()
	prev := &PointerEntity{ID: "1", Score: intPtr(42)}
	curr := &PointerEntity{ID: "1", Score: intPtr(100)}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
}
