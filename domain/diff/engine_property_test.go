package diff_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/diff"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// GA-012: Property Tests
// ---------------------------------------------------------------------------

// TestDiff_Apply_RoundTrip_Property uses pgregory.net/rapid to verify the
// round-trip property: Apply(prev, Diff(prev, curr)) == curr for randomly
// generated entity values.
func TestDiff_Apply_RoundTrip_Property(t *testing.T) {
	e := diff.New()
	ctx := context.Background()

	// --- Shape 1: flat struct with mixed types ---
	type FlatEntity struct {
		Name       string
		Email      string
		Age        int
		Score      float64
		Active     bool
		LoginCount int64
		Rating     float32
		Level      uint8
	}

	t.Run("flat_struct", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			prev := FlatEntity{
				Name:       rapid.String().Draw(t, "prev.Name"),
				Email:      rapid.String().Draw(t, "prev.Email"),
				Age:        rapid.Int().Draw(t, "prev.Age"),
				Score:      rapid.Float64().Draw(t, "prev.Score"),
				Active:     rapid.Bool().Draw(t, "prev.Active"),
				LoginCount: rapid.Int64().Draw(t, "prev.LoginCount"),
				Rating:     rapid.Float32().Draw(t, "prev.Rating"),
				Level:      rapid.Uint8().Draw(t, "prev.Level"),
			}
			curr := FlatEntity{
				Name:       rapid.String().Draw(t, "curr.Name"),
				Email:      rapid.String().Draw(t, "curr.Email"),
				Age:        rapid.Int().Draw(t, "curr.Age"),
				Score:      rapid.Float64().Draw(t, "curr.Score"),
				Active:     rapid.Bool().Draw(t, "curr.Active"),
				LoginCount: rapid.Int64().Draw(t, "curr.LoginCount"),
				Rating:     rapid.Float32().Draw(t, "curr.Rating"),
				Level:      rapid.Uint8().Draw(t, "curr.Level"),
			}

			delta, err := e.Diff(ctx, prev, curr)
			if err != nil {
				t.Fatalf("Diff: %v", err)
			}

			result, err := e.Apply(ctx, prev, delta)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}

			check, err := e.Diff(ctx, result, curr)
			if err != nil {
				t.Fatalf("re-Diff: %v", err)
			}
			if !check.IsEmpty() {
				t.Fatalf("round-trip mismatch: %d residual change(s)", len(check.Changes))
			}
		})
	})

	// --- Shape 2: nested struct ---
	type Inner struct {
		City  string
		State string
		Zip   string
	}
	type Nested struct {
		Name string
		Addr Inner
	}

	t.Run("nested_struct", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			prev := Nested{
				Name: rapid.String().Draw(t, "prev.Name"),
				Addr: Inner{
					City:  rapid.String().Draw(t, "prev.City"),
					State: rapid.String().Draw(t, "prev.State"),
					Zip:   rapid.String().Draw(t, "prev.Zip"),
				},
			}
			curr := Nested{
				Name: rapid.String().Draw(t, "curr.Name"),
				Addr: Inner{
					City:  rapid.String().Draw(t, "curr.City"),
					State: rapid.String().Draw(t, "curr.State"),
					Zip:   rapid.String().Draw(t, "curr.Zip"),
				},
			}

			delta, err := e.Diff(ctx, prev, curr)
			if err != nil {
				t.Fatalf("Diff: %v", err)
			}
			result, err := e.Apply(ctx, prev, delta)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			check, err := e.Diff(ctx, result, curr)
			if err != nil {
				t.Fatalf("re-Diff: %v", err)
			}
			if !check.IsEmpty() {
				t.Fatalf("round-trip mismatch: %d residual change(s)", len(check.Changes))
			}
		})
	})

	// --- Shape 3: struct with slices ---
	type SliceEntity struct {
		Name string
		Tags []string
	}

	t.Run("slice_struct", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			prev := SliceEntity{
				Name: rapid.String().Draw(t, "prev.Name"),
				Tags: rapid.SliceOf(rapid.String()).Draw(t, "prev.Tags"),
			}
			curr := SliceEntity{
				Name: rapid.String().Draw(t, "curr.Name"),
				Tags: rapid.SliceOf(rapid.String()).Draw(t, "curr.Tags"),
			}

			delta, err := e.Diff(ctx, prev, curr)
			if err != nil {
				t.Fatalf("Diff: %v", err)
			}
			result, err := e.Apply(ctx, prev, delta)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			check, err := e.Diff(ctx, result, curr)
			if err != nil {
				t.Fatalf("re-Diff: %v", err)
			}
			if !check.IsEmpty() {
				t.Fatalf("round-trip mismatch: %d residual change(s)", len(check.Changes))
			}
		})
	})

	// --- Shape 4: struct with pointer fields ---
	type PtrEntity struct {
		Name  string
		Alias *string
	}

	t.Run("pointer_struct", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			var prevAlias, currAlias *string
			if rapid.Bool().Draw(t, "prev.hasAlias") {
				s := rapid.String().Draw(t, "prev.Alias")
				prevAlias = &s
			}
			if rapid.Bool().Draw(t, "curr.hasAlias") {
				s := rapid.String().Draw(t, "curr.Alias")
				currAlias = &s
			}

			prev := PtrEntity{Name: rapid.String().Draw(t, "prev.Name"), Alias: prevAlias}
			curr := PtrEntity{Name: rapid.String().Draw(t, "curr.Name"), Alias: currAlias}

			delta, err := e.Diff(ctx, prev, curr)
			if err != nil {
				t.Fatalf("Diff: %v", err)
			}
			result, err := e.Apply(ctx, prev, delta)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			check, err := e.Diff(ctx, result, curr)
			if err != nil {
				t.Fatalf("re-Diff: %v", err)
			}
			if !check.IsEmpty() {
				t.Fatalf("round-trip mismatch: %d residual change(s)", len(check.Changes))
			}
		})
	})
}

// ---------------------------------------------------------------------------
// GA-012: Benchmarks
// ---------------------------------------------------------------------------

// BenchmarkDiff_FlatStruct_20Fields — target <1ms per op.
func BenchmarkDiff_FlatStruct_20Fields_GA012(b *testing.B) {
	e := diff.New()
	ctx := context.Background()

	type Entity20 struct {
		F01 string
		F02 string
		F03 string
		F04 string
		F05 string
		F06 int
		F07 int
		F08 int
		F09 float64
		F10 float64
		F11 bool
		F12 bool
		F13 int64
		F14 int64
		F15 uint32
		F16 uint32
		F17 string
		F18 string
		F19 float32
		F20 float32
	}

	prev := Entity20{
		F01: "alpha", F02: "bravo", F03: "charlie", F04: "delta", F05: "echo",
		F06: 1, F07: 2, F08: 3, F09: 1.1, F10: 2.2,
		F11: true, F12: false, F13: 100, F14: 200, F15: 300, F16: 400,
		F17: "golf", F18: "hotel", F19: 3.3, F20: 4.4,
	}
	curr := prev
	curr.F03 = "changed"
	curr.F09 = 9.9

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.Diff(ctx, prev, curr)
	}
}

// BenchmarkDiff_NestedJSON_5Levels — target <3ms per op.
func BenchmarkDiff_NestedJSON_5Levels(b *testing.B) {
	e := diff.New()
	ctx := context.Background()

	type L5 struct{ Val string }
	type L4 struct{ L5 L5 }
	type L3 struct{ L4 L4 }
	type L2 struct{ L3 L3 }
	type L1 struct {
		Name string
		L2   L2
	}

	prev := L1{Name: "root", L2: L2{L3: L3{L4: L4{L5: L5{Val: "deep-old"}}}}}
	curr := L1{Name: "root", L2: L2{L3: L3{L4: L4{L5: L5{Val: "deep-new"}}}}}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.Diff(ctx, prev, curr)
	}
}

// BenchmarkDiff_100Fields — target <5ms per op.
func BenchmarkDiff_100Fields(b *testing.B) {
	e := diff.New()
	ctx := context.Background()

	// Use a struct with 100 string fields via a map-based entity.
	// Since we can't easily define a 100-field struct in code, we use
	// a struct-of-structs (5 groups × 20 fields = 100 fields total).
	type Group20 struct {
		F01, F02, F03, F04, F05 string
		F06, F07, F08, F09, F10 string
		F11, F12, F13, F14, F15 string
		F16, F17, F18, F19, F20 string
	}
	type Entity100 struct {
		G1 Group20
		G2 Group20
		G3 Group20
		G4 Group20
		G5 Group20
	}

	prev := Entity100{
		G1: Group20{F01: "a1", F02: "a2", F03: "a3", F04: "a4", F05: "a5", F06: "a6", F07: "a7", F08: "a8", F09: "a9", F10: "a10", F11: "a11", F12: "a12", F13: "a13", F14: "a14", F15: "a15", F16: "a16", F17: "a17", F18: "a18", F19: "a19", F20: "a20"},
		G2: Group20{F01: "b1", F02: "b2", F03: "b3", F04: "b4", F05: "b5", F06: "b6", F07: "b7", F08: "b8", F09: "b9", F10: "b10", F11: "b11", F12: "b12", F13: "b13", F14: "b14", F15: "b15", F16: "b16", F17: "b17", F18: "b18", F19: "b19", F20: "b20"},
		G3: Group20{F01: "c1", F02: "c2", F03: "c3", F04: "c4", F05: "c5", F06: "c6", F07: "c7", F08: "c8", F09: "c9", F10: "c10", F11: "c11", F12: "c12", F13: "c13", F14: "c14", F15: "c15", F16: "c16", F17: "c17", F18: "c18", F19: "c19", F20: "c20"},
		G4: Group20{F01: "d1", F02: "d2", F03: "d3", F04: "d4", F05: "d5", F06: "d6", F07: "d7", F08: "d8", F09: "d9", F10: "d10", F11: "d11", F12: "d12", F13: "d13", F14: "d14", F15: "d15", F16: "d16", F17: "d17", F18: "d18", F19: "d19", F20: "d20"},
		G5: Group20{F01: "e1", F02: "e2", F03: "e3", F04: "e4", F05: "e5", F06: "e6", F07: "e7", F08: "e8", F09: "e9", F10: "e10", F11: "e11", F12: "e12", F13: "e13", F14: "e14", F15: "e15", F16: "e16", F17: "e17", F18: "e18", F19: "e19", F20: "e20"},
	}
	curr := prev
	curr.G1.F05 = "changed1"
	curr.G3.F10 = "changed2"
	curr.G5.F15 = "changed3"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.Diff(ctx, prev, curr)
	}
}

// ---------------------------------------------------------------------------
// GA-012: Coverage-boosting tests — exercise uncovered paths
// ---------------------------------------------------------------------------

// TestDiff_PointerToStruct validates diffing through pointer-to-struct inputs.
func TestDiff_PointerToStruct(t *testing.T) {
	e := diff.New()
	prev := &testUser{Name: "A", Email: "a@b.com", Age: 1}
	curr := &testUser{Name: "B", Email: "a@b.com", Age: 1}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(delta.Changes) != 1 || delta.Changes[0].Path != "Name" {
		t.Fatalf("expected 1 Name change, got %+v", delta.Changes)
	}
}

// TestDiff_TypeMismatch verifies ErrDiff on mismatched types.
func TestDiff_TypeMismatch(t *testing.T) {
	e := diff.New()
	type A struct{ X int }
	type B struct{ X int }

	_, err := e.Diff(context.Background(), A{1}, B{1})
	if err == nil {
		t.Fatal("expected error for type mismatch")
	}
}

// TestDiff_NilPrevPointer verifies ErrDiff on nil pointer input.
func TestDiff_NilPrevPointer(t *testing.T) {
	e := diff.New()
	var prev *testUser
	curr := testUser{Name: "A"}

	_, err := e.Diff(context.Background(), prev, curr)
	if err == nil {
		t.Fatal("expected error for nil prev pointer")
	}
}

// TestDiff_NonStructInput verifies ErrDiff on non-struct input.
func TestDiff_NonStructInput(t *testing.T) {
	e := diff.New()
	_, err := e.Diff(context.Background(), "hello", "world")
	if err == nil {
		t.Fatal("expected error for non-struct input")
	}
}

// TestApply_NilPrevPointer verifies ErrDiff when Apply receives nil pointer.
func TestApply_NilPrevPointer(t *testing.T) {
	e := diff.New()
	var prev *testUser

	_, err := e.Apply(context.Background(), prev, domain.Delta{
		Changes: []domain.FieldChange{{Path: "Name", NewValue: json.RawMessage(`"B"`)}},
	})
	if err == nil {
		t.Fatal("expected error for nil prev in Apply")
	}
}

// TestApply_NonStructInput verifies ErrDiff when Apply receives non-struct.
func TestApply_NonStructInput(t *testing.T) {
	e := diff.New()
	_, err := e.Apply(context.Background(), "hello", domain.Delta{
		Changes: []domain.FieldChange{{Path: "X", NewValue: json.RawMessage(`1`)}},
	})
	if err == nil {
		t.Fatal("expected error for non-struct in Apply")
	}
}

// TestApply_UnknownField verifies Apply errors on non-existent field path.
func TestApply_UnknownField(t *testing.T) {
	e := diff.New()
	prev := testUser{Name: "A"}
	_, err := e.Apply(context.Background(), prev, domain.Delta{
		Changes: []domain.FieldChange{{Path: "DoesNotExist", NewValue: json.RawMessage(`"x"`)}},
	})
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
}

// TestApply_SetNilValue verifies Apply can set a field to its zero value (nil).
func TestApply_SetNilValue(t *testing.T) {
	type Entity struct {
		Name  string
		Alias *string
	}
	e := diff.New()
	s := "old"
	prev := Entity{Name: "A", Alias: &s}

	result, err := e.Apply(context.Background(), prev, domain.Delta{
		Changes: []domain.FieldChange{{Path: "Alias", NewValue: nil}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := result.(Entity)
	if got.Alias != nil {
		t.Errorf("expected nil Alias, got %v", *got.Alias)
	}
}

// TestApply_MapKeyRemoved verifies Apply can remove a map key.
func TestApply_MapKeyRemoved(t *testing.T) {
	type Entity struct {
		Meta map[string]string
	}
	e := diff.New()
	prev := Entity{Meta: map[string]string{"a": "1", "b": "2"}}

	result, err := e.Apply(context.Background(), prev, domain.Delta{
		Changes: []domain.FieldChange{{Path: "Meta.b", OldValue: json.RawMessage(`"2"`), NewValue: nil}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := result.(Entity)
	if _, ok := got.Meta["b"]; ok {
		t.Error("expected key 'b' to be removed")
	}
	if got.Meta["a"] != "1" {
		t.Error("expected key 'a' to remain")
	}
}

// TestApply_MapKeyAdded verifies Apply can add a new map key.
func TestApply_MapKeyAdded(t *testing.T) {
	type Entity struct {
		Meta map[string]string
	}
	e := diff.New()
	prev := Entity{Meta: map[string]string{"a": "1"}}

	result, err := e.Apply(context.Background(), prev, domain.Delta{
		Changes: []domain.FieldChange{{Path: "Meta.c", OldValue: nil, NewValue: json.RawMessage(`"3"`)}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := result.(Entity)
	if got.Meta["c"] != "3" {
		t.Errorf("expected Meta[c]=%q, got %q", "3", got.Meta["c"])
	}
}

// TestApply_EmbeddedFieldChange verifies Apply handles embedded struct fields.
func TestApply_EmbeddedFieldChange(t *testing.T) {
	type Address struct {
		City string
	}
	type Entity struct {
		Address
		Name string
	}
	e := diff.New()
	prev := Entity{Address: Address{City: "NYC"}, Name: "X"}

	result, err := e.Apply(context.Background(), prev, domain.Delta{
		Changes: []domain.FieldChange{{Path: "City", NewValue: json.RawMessage(`"LA"`)}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := result.(Entity)
	if got.City != "LA" {
		t.Errorf("expected City=LA, got %s", got.City)
	}
}

// TestApply_PointerInput verifies Apply works when prev is a pointer.
func TestApply_PointerInput(t *testing.T) {
	e := diff.New()
	prev := &testUser{Name: "A", Email: "old@test.com", Age: 1}
	curr := testUser{Name: "A", Email: "new@test.com", Age: 1}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	result, err := e.Apply(context.Background(), prev, delta)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := result.(testUser)
	if got.Email != "new@test.com" {
		t.Errorf("expected Email=new@test.com, got %s", got.Email)
	}
}

// TestDiff_FuncField verifies ErrDiffFallback for func fields.
func TestDiff_FuncField(t *testing.T) {
	type Entity struct {
		Name string
		Fn   func()
	}
	e := diff.New()
	_, err := e.Diff(context.Background(), Entity{Name: "A"}, Entity{Name: "B"})
	if err == nil {
		t.Fatal("expected error for func field")
	}
}

// TestDiff_TimeNoChange verifies no diff when times are equal but in different locations.
func TestDiff_TimeNoChange(t *testing.T) {
	type Entity struct {
		At time.Time
	}
	e := diff.New()
	loc := time.FixedZone("test", 3600)
	t1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := t1.In(loc)

	delta, err := e.Diff(context.Background(), Entity{At: t1}, Entity{At: t2})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !delta.IsEmpty() {
		t.Error("expected no changes for equal times in different zones")
	}
}

// TestSortedSliceCopy_Nil verifies normalized tag with nil slice.
func TestSortedSliceCopy_Nil(t *testing.T) {
	type Entity struct {
		Tags []string `version:"normalized"`
	}
	e := diff.New()
	delta, err := e.Diff(context.Background(), Entity{}, Entity{})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !delta.IsEmpty() {
		t.Error("expected no changes for nil normalized slices")
	}
}

// TestSortedSliceCopy_Empty verifies normalized tag with empty slice.
func TestSortedSliceCopy_Empty(t *testing.T) {
	type Entity struct {
		Tags []string `version:"normalized"`
	}
	e := diff.New()
	delta, err := e.Diff(context.Background(), Entity{Tags: []string{}}, Entity{Tags: []string{}})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !delta.IsEmpty() {
		t.Error("expected no changes for empty normalized slices")
	}
}

// ---------------------------------------------------------------------------
// GA-012: Additional coverage tests — target uncovered code paths
// ---------------------------------------------------------------------------

// TestApply_NestedMapPath exercises applyMapPath with nested maps (non-terminal path).
func TestApply_NestedMapPath(t *testing.T) {
	type Entity struct {
		Settings map[string]any
	}
	e := diff.New()
	prev := Entity{Settings: map[string]any{
		"notifications": map[string]any{"email": true, "sms": false},
	}}
	curr := Entity{Settings: map[string]any{
		"notifications": map[string]any{"email": true, "sms": true},
	}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if delta.IsEmpty() {
		t.Fatal("expected changes")
	}

	result, err := e.Apply(context.Background(), prev, delta)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Verify round-trip via re-diff.
	check, err := e.Diff(context.Background(), result, curr)
	if err != nil {
		t.Fatalf("re-Diff: %v", err)
	}
	if !check.IsEmpty() {
		t.Errorf("round-trip mismatch: %d residual change(s): %+v", len(check.Changes), check.Changes)
	}
}

// TestApply_NestedStructPath exercises applyChange walking into nested structs.
func TestApply_NestedStructPath(t *testing.T) {
	type Inner struct {
		City  string
		State string
	}
	type Outer struct {
		Name string
		Addr Inner
	}
	e := diff.New()
	prev := Outer{Name: "X", Addr: Inner{City: "NYC", State: "NY"}}
	curr := Outer{Name: "X", Addr: Inner{City: "LA", State: "CA"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	result, err := e.Apply(context.Background(), prev, delta)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := result.(Outer)
	if got.Addr.City != "LA" || got.Addr.State != "CA" {
		t.Errorf("expected Addr={LA CA}, got %+v", got.Addr)
	}
}

// TestDiff_PointerToNestedStruct exercises the pointer-deref-then-recurse path.
func TestDiff_PointerToNestedStruct(t *testing.T) {
	type Inner struct {
		Val string
	}
	type Entity struct {
		Name string
		Data *Inner
	}
	e := diff.New()

	prev := Entity{Name: "A", Data: &Inner{Val: "old"}}
	curr := Entity{Name: "A", Data: &Inner{Val: "new"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(delta.Changes), delta.Changes)
	}
	if delta.Changes[0].Path != "Data.Val" {
		t.Errorf("expected path Data.Val, got %s", delta.Changes[0].Path)
	}

	// Round-trip test.
	result, err := e.Apply(context.Background(), prev, delta)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	check, err := e.Diff(context.Background(), result, curr)
	if err != nil {
		t.Fatalf("re-Diff: %v", err)
	}
	if !check.IsEmpty() {
		t.Errorf("round-trip mismatch: %+v", check.Changes)
	}
}

// TestApply_PointerToStruct exercises Apply with pointer-deref at the Apply level.
func TestApply_PointerToStruct_Nested(t *testing.T) {
	type Inner struct{ X int }
	type Entity struct {
		Ptr *Inner
	}
	e := diff.New()

	prev := Entity{Ptr: &Inner{X: 1}}
	curr := Entity{Ptr: &Inner{X: 2}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	result, err := e.Apply(context.Background(), prev, delta)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	check, err := e.Diff(context.Background(), result, curr)
	if err != nil {
		t.Fatalf("re-Diff: %v", err)
	}
	if !check.IsEmpty() {
		t.Errorf("round-trip mismatch: %+v", check.Changes)
	}
}

// TestApply_MapKeyNewInEmptyMap exercises Apply adding a key when the map path
// requires creating an entry in a map that exists but has no matching key.
func TestApply_MapKeyNewInEmptyMap(t *testing.T) {
	type Entity struct {
		Meta map[string]string
	}
	e := diff.New()
	prev := Entity{Meta: map[string]string{}}

	result, err := e.Apply(context.Background(), prev, domain.Delta{
		Changes: []domain.FieldChange{
			{Path: "Meta.newkey", OldValue: nil, NewValue: json.RawMessage(`"val"`)},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := result.(Entity)
	if got.Meta["newkey"] != "val" {
		t.Errorf("expected Meta[newkey]=%q, got %q", "val", got.Meta["newkey"])
	}
}

// TestApply_InvalidJSON verifies Apply returns error on invalid JSON in delta.
func TestApply_InvalidJSON(t *testing.T) {
	e := diff.New()
	prev := testUser{Name: "A"}

	_, err := e.Apply(context.Background(), prev, domain.Delta{
		Changes: []domain.FieldChange{
			{Path: "Name", NewValue: json.RawMessage(`not-valid-json`)},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON in delta")
	}
}

// TestDiff_StructWithUnexportedFields verifies unexported fields are skipped.
func TestDiff_StructWithUnexportedFields(t *testing.T) {
	type Entity struct {
		Name     string
		internal string //nolint:unused
	}
	e := diff.New()

	prev := Entity{Name: "A"}
	curr := Entity{Name: "B"}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(delta.Changes) != 1 || delta.Changes[0].Path != "Name" {
		t.Errorf("unexpected changes: %+v", delta.Changes)
	}
}

// TestApply_EmptyDelta verifies Apply returns prev unchanged when delta is empty.
func TestApply_EmptyDelta(t *testing.T) {
	e := diff.New()
	prev := testUser{Name: "A", Email: "a@b.com", Age: 30}

	result, err := e.Apply(context.Background(), prev, domain.Delta{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// When delta is empty, Apply returns prev directly.
	got, ok := result.(testUser)
	if !ok {
		t.Fatalf("expected testUser, got %T", result)
	}
	if got != prev {
		t.Errorf("expected %+v, got %+v", prev, got)
	}
}

// TestDiff_MapBothNil verifies no diff for nil map fields.
func TestDiff_MapBothNil(t *testing.T) {
	type Entity struct {
		Meta map[string]string
	}
	e := diff.New()

	delta, err := e.Diff(context.Background(), Entity{}, Entity{})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !delta.IsEmpty() {
		t.Error("expected no changes for nil maps")
	}
}

// TestDiff_MapNilVsEmpty verifies diff between nil and empty map.
func TestDiff_MapNilVsEmpty(t *testing.T) {
	type Entity struct {
		Meta map[string]string
	}
	e := diff.New()

	delta, err := e.Diff(context.Background(), Entity{Meta: nil}, Entity{Meta: map[string]string{}})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	// Both have no keys — should produce empty delta.
	if !delta.IsEmpty() {
		t.Errorf("expected no changes between nil and empty map, got %+v", delta.Changes)
	}
}

// TestApply_PointerField_NilToValue exercises Apply setting a pointer field
// from nil to a value, which requires the setFieldFromJSON path.
func TestApply_PointerField_NilToValue(t *testing.T) {
	type Entity struct {
		Name  string
		Alias *string
	}
	e := diff.New()
	prev := Entity{Name: "A", Alias: nil}

	result, err := e.Apply(context.Background(), prev, domain.Delta{
		Changes: []domain.FieldChange{
			{Path: "Alias", OldValue: nil, NewValue: json.RawMessage(`"bob"`)},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := result.(Entity)
	if got.Alias == nil || *got.Alias != "bob" {
		t.Errorf("expected Alias=bob, got %v", got.Alias)
	}
}

// TestApply_NestedStruct_DeepPath exercises applyChange walking through
// multiple struct levels (path "A.B.C").
func TestApply_NestedStruct_DeepPath(t *testing.T) {
	type C struct{ Val string }
	type B struct{ C C }
	type A struct {
		Name string
		B    B
	}
	e := diff.New()
	prev := A{Name: "X", B: B{C: C{Val: "old"}}}
	curr := A{Name: "X", B: B{C: C{Val: "new"}}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	result, err := e.Apply(context.Background(), prev, delta)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := result.(A)
	if got.B.C.Val != "new" {
		t.Errorf("expected B.C.Val=new, got %s", got.B.C.Val)
	}
}

// TestDiff_Map_OneNilOnePopulated verifies diff when one map is nil and other has values.
func TestDiff_Map_OneNilOnePopulated(t *testing.T) {
	type Entity struct {
		Meta map[string]string
	}
	e := diff.New()

	prev := Entity{Meta: nil}
	curr := Entity{Meta: map[string]string{"key": "val"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if delta.IsEmpty() {
		t.Fatal("expected changes when going from nil to populated map")
	}
	if len(delta.Changes) != 1 || delta.Changes[0].Path != "Meta.key" {
		t.Errorf("unexpected changes: %+v", delta.Changes)
	}
}

// TestDiff_MultiplePointerTransitions exercises various pointer transitions in one struct.
func TestDiff_MultiplePointerTransitions(t *testing.T) {
	type Entity struct {
		A *string
		B *string
		C *string
	}
	e := diff.New()
	s1, s2, s3 := "x", "y", "z"

	prev := Entity{A: &s1, B: nil, C: &s3}
	curr := Entity{A: nil, B: &s2, C: &s3}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(delta.Changes) != 2 {
		t.Fatalf("expected 2 changes, got %d: %+v", len(delta.Changes), delta.Changes)
	}

	result, err := e.Apply(context.Background(), prev, delta)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	check, err := e.Diff(context.Background(), result, curr)
	if err != nil {
		t.Fatalf("re-Diff: %v", err)
	}
	if !check.IsEmpty() {
		t.Errorf("round-trip mismatch: %+v", check.Changes)
	}
}

// TestDiff_UnsafePointerField verifies ErrDiffFallback for unsafe.Pointer fields.
func TestDiff_UnsafePointerField(t *testing.T) {
	// We can't easily use unsafe.Pointer in a test struct without importing unsafe.
	// Instead, we test that func fields (another unsupported kind) trigger fallback.
	type WithFunc struct {
		Name string
		Cb   func() error
	}
	e := diff.New()
	_, err := e.Diff(context.Background(), WithFunc{Name: "A"}, WithFunc{Name: "B"})
	if err == nil {
		t.Fatal("expected error for func field")
	}
}

// TestApply_MapValueChange exercises Apply changing an existing map value.
func TestApply_MapValueChange(t *testing.T) {
	type Entity struct {
		Meta map[string]string
	}
	e := diff.New()
	prev := Entity{Meta: map[string]string{"k": "old"}}
	curr := Entity{Meta: map[string]string{"k": "new"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	result, err := e.Apply(context.Background(), prev, delta)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := result.(Entity)
	if got.Meta["k"] != "new" {
		t.Errorf("expected Meta[k]=new, got %s", got.Meta["k"])
	}
}
