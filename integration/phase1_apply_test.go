//go:build integration

package integration

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain/diff"
)

// ---------------------------------------------------------------------------
// Phase 1 — A-001 to A-015: Apply (reconstruct from delta) tests
// ---------------------------------------------------------------------------

// applyAndCompare is a helper that diffs prev→curr, applies the delta to prev,
// and verifies the result matches curr.
func applyAndCompare(t *testing.T, e *diff.Engine, prev, curr any) {
	t.Helper()
	ctx := context.Background()

	delta, err := e.Diff(ctx, prev, curr)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	result, err := e.Apply(ctx, prev, delta)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// After applying, Diff(result, curr) should be empty.
	verifyDelta, err := e.Diff(ctx, result, curr)
	if err != nil {
		t.Fatalf("Verification Diff: %v", err)
	}
	if !verifyDelta.IsEmpty() {
		t.Errorf("Apply did not reproduce curr — remaining %d changes: %+v",
			len(verifyDelta.Changes), verifyDelta.Changes)
	}
}

// A-001: TestApply_Flat_SingleField applies a single Name change to FlatEntity.
func TestApply_Flat_SingleField(t *testing.T) {
	e := diff.New()
	prev := &FlatEntity{ID: "1", Name: "Alice", Age: 25, Score: 95.5, Active: true}
	curr := &FlatEntity{ID: "1", Name: "Bob", Age: 25, Score: 95.5, Active: true}
	applyAndCompare(t, e, prev, curr)
}

// A-002: TestApply_Flat_AllFields applies a delta with all fields changed.
func TestApply_Flat_AllFields(t *testing.T) {
	e := diff.New()
	prev := &FlatEntity{ID: "1", Name: "Alice", Age: 25, Score: 95.5, Active: true}
	curr := &FlatEntity{ID: "1", Name: "Bob", Age: 30, Score: 97.2, Active: false}
	applyAndCompare(t, e, prev, curr)
}

// A-003: TestApply_Nested_LeafField applies Address.City change.
func TestApply_Nested_LeafField(t *testing.T) {
	e := diff.New()
	prev := &NestedEntity{
		ID:      "1",
		Name:    "Alice",
		Address: Address{Street: "1st St", City: "NYC", Country: "US", ZipCode: "10001"},
	}
	curr := &NestedEntity{
		ID:      "1",
		Name:    "Alice",
		Address: Address{Street: "1st St", City: "LA", Country: "US", ZipCode: "10001"},
	}
	applyAndCompare(t, e, prev, curr)
}

// A-004: TestApply_DeepNest_4Level applies Coord.Lat change.
func TestApply_DeepNest_4Level(t *testing.T) {
	e := diff.New()
	prev := &Company{
		ID: "1", Name: "Acme",
		Headquarters: Office{
			Name:     "HQ",
			Location: Location{Label: "Main", Coord: GeoCoord{Lat: 40.7, Lng: -74.0}},
		},
	}
	curr := &Company{
		ID: "1", Name: "Acme",
		Headquarters: Office{
			Name:     "HQ",
			Location: Location{Label: "Main", Coord: GeoCoord{Lat: 34.0, Lng: -74.0}},
		},
	}
	applyAndCompare(t, e, prev, curr)
}

// A-005: TestApply_Map_AddKey applies map key addition.
func TestApply_Map_AddKey(t *testing.T) {
	e := diff.New()
	prev := &MapEntity{ID: "1", Tags: map[string]string{}}
	curr := &MapEntity{ID: "1", Tags: map[string]string{"role": "admin"}}
	applyAndCompare(t, e, prev, curr)
}

// A-006: TestApply_Map_RemoveKey applies map key removal.
func TestApply_Map_RemoveKey(t *testing.T) {
	e := diff.New()
	prev := &MapEntity{ID: "1", Tags: map[string]string{"role": "admin"}}
	curr := &MapEntity{ID: "1", Tags: map[string]string{}}
	applyAndCompare(t, e, prev, curr)
}

// A-007: TestApply_Map_NestedMap applies nested map change.
func TestApply_Map_NestedMap(t *testing.T) {
	e := diff.New()
	prev := &MapEntity{
		ID: "1",
		Nested: map[string]map[string]string{
			"g1": {"k1": "v1"},
		},
	}
	curr := &MapEntity{
		ID: "1",
		Nested: map[string]map[string]string{
			"g1": {"k1": "v2"},
		},
	}
	applyAndCompare(t, e, prev, curr)
}

// A-008: TestApply_Slice_Change applies slice content change.
func TestApply_Slice_Change(t *testing.T) {
	e := diff.New()
	prev := &SliceEntity{ID: "1", Roles: []string{"a", "b"}}
	curr := &SliceEntity{ID: "1", Roles: []string{"a", "c"}}
	applyAndCompare(t, e, prev, curr)
}

// A-009: TestApply_Ptr_NilToValue applies nil→*string transition.
func TestApply_Ptr_NilToValue(t *testing.T) {
	e := diff.New()
	prev := &PointerEntity{ID: "1", Nickname: nil}
	curr := &PointerEntity{ID: "1", Nickname: strPtr("Bob")}
	applyAndCompare(t, e, prev, curr)
}

// A-010: TestApply_Ptr_ValueToNil applies *string→nil transition.
func TestApply_Ptr_ValueToNil(t *testing.T) {
	e := diff.New()
	prev := &PointerEntity{ID: "1", Nickname: strPtr("Bob")}
	curr := &PointerEntity{ID: "1", Nickname: nil}
	applyAndCompare(t, e, prev, curr)
}

// A-011: TestApply_Ptr_StructLeaf applies change through pointer-to-struct.
func TestApply_Ptr_StructLeaf(t *testing.T) {
	e := diff.New()
	prev := &PointerEntity{ID: "1", Address: &Address{City: "NYC"}}
	curr := &PointerEntity{ID: "1", Address: &Address{City: "LA"}}
	applyAndCompare(t, e, prev, curr)
}

// A-012: TestApply_EmptyDelta applies an empty delta and verifies identity.
func TestApply_EmptyDelta(t *testing.T) {
	e := diff.New()
	ctx := context.Background()
	prev := &FlatEntity{ID: "1", Name: "Alice", Age: 25}

	delta, err := e.Diff(ctx, prev, prev) // identical → empty delta
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !delta.IsEmpty() {
		t.Fatalf("expected empty delta")
	}

	result, err := e.Apply(ctx, prev, delta)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Result should be deeply equal to prev.
	if !reflect.DeepEqual(result, prev) {
		t.Errorf("Apply of empty delta should return identical entity")
	}
}

// A-013: TestApply_Time_Change applies time.Time field change.
func TestApply_Time_Change(t *testing.T) {
	e := diff.New()
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	prev := &TimeEntity{ID: "1", CreatedAt: now, UpdatedAt: now}
	curr := &TimeEntity{ID: "1", CreatedAt: now.Add(time.Hour), UpdatedAt: now}
	applyAndCompare(t, e, prev, curr)
}

// A-014: TestApply_Embedded_Promoted applies promoted (embedded) field change.
func TestApply_Embedded_Promoted(t *testing.T) {
	e := diff.New()
	prev := &EmbeddedEntity{ID: "1", Name: "Alice", AuditFields: AuditFields{CreatedBy: "a"}}
	curr := &EmbeddedEntity{ID: "1", Name: "Alice", AuditFields: AuditFields{CreatedBy: "b"}}
	applyAndCompare(t, e, prev, curr)
}

// A-015: TestApply_RoundTrip_Full does Diff→Apply→Diff and verifies empty final delta.
func TestApply_RoundTrip_Full(t *testing.T) {
	e := diff.New()
	ctx := context.Background()

	prev := &UserEntity{
		ID: "1", Name: "Alice", Email: "a@test.com", Age: 25,
		Address:  Address{City: "NYC", Country: "US"},
		Roles:    []string{"admin"},
		Settings: map[string]string{"theme": "dark"},
	}
	curr := &UserEntity{
		ID: "1", Name: "Bob", Email: "b@test.com", Age: 30,
		Address:  Address{City: "LA", Country: "US"},
		Roles:    []string{"user", "viewer"},
		Settings: map[string]string{"theme": "light", "lang": "en"},
	}

	// Step 1: Diff
	delta, err := e.Diff(ctx, prev, curr)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if delta.IsEmpty() {
		t.Fatal("expected non-empty delta")
	}

	// Step 2: Apply
	result, err := e.Apply(ctx, prev, delta)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Step 3: Diff(result, curr) should be empty — round-trip consistency.
	finalDelta, err := e.Diff(ctx, result, curr)
	if err != nil {
		t.Fatalf("Final Diff: %v", err)
	}
	if !finalDelta.IsEmpty() {
		t.Errorf("round-trip failed — %d residual changes: %+v",
			len(finalDelta.Changes), finalDelta.Changes)
	}
}
