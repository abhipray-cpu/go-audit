package cloner_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/abhipray-cpu/go-audit/adapter/cloner"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time interface check.
var _ port.ClonerPort = (*cloner.ReflectCloner)(nil)

// --- helper types ---

type Address struct {
	Street string
	City   string
}

type Person struct {
	Name    string
	Age     int
	Address *Address
	Tags    []string
	Meta    map[string]string
	NilPtr  *Address
}

// --- GA-016 tests ---

func TestReflectCloner_DeepCopy(t *testing.T) {
	c := cloner.NewReflect()
	ctx := context.Background()

	orig := &Person{
		Name:    "Alice",
		Age:     30,
		Address: &Address{Street: "123 Main", City: "Springfield"},
		Tags:    []string{"admin", "user"},
		Meta:    map[string]string{"role": "lead"},
	}

	raw, err := c.Clone(ctx, orig)
	if err != nil {
		t.Fatalf("Clone failed: %v", err)
	}
	got, ok := raw.(*Person)
	if !ok {
		t.Fatalf("expected *Person, got %T", raw)
	}

	// Values should be equal.
	if !reflect.DeepEqual(orig, got) {
		t.Fatalf("clone not equal:\n  orig: %+v\n  got:  %+v", orig, got)
	}

	// Mutate original — clone must stay unchanged.
	orig.Name = "Bob"
	orig.Age = 99
	orig.Address.City = "Shelbyville"
	orig.Tags[0] = "revoked"
	orig.Meta["role"] = "viewer"

	if got.Name != "Alice" {
		t.Error("clone name mutated")
	}
	if got.Age != 30 {
		t.Error("clone age mutated")
	}
	if got.Address.City != "Springfield" {
		t.Error("clone address mutated")
	}
	if got.Tags[0] != "admin" {
		t.Error("clone tags mutated")
	}
	if got.Meta["role"] != "lead" {
		t.Error("clone meta mutated")
	}
}

func TestReflectCloner_NestedPointers(t *testing.T) {
	c := cloner.NewReflect()
	ctx := context.Background()

	inner := &Address{Street: "Nested St", City: "Deep City"}
	orig := &Person{
		Name:    "Carol",
		Address: inner,
	}

	raw, err := c.Clone(ctx, orig)
	if err != nil {
		t.Fatalf("Clone failed: %v", err)
	}
	got := raw.(*Person)

	// Pointers must be different allocations.
	if got.Address == orig.Address {
		t.Fatal("clone shares the same Address pointer")
	}

	// But values equal.
	if !reflect.DeepEqual(orig.Address, got.Address) {
		t.Error("cloned address values differ")
	}
}

func TestReflectCloner_NilFields(t *testing.T) {
	c := cloner.NewReflect()
	ctx := context.Background()

	orig := &Person{
		Name:   "Dave",
		NilPtr: nil,
		Tags:   nil,
		Meta:   nil,
	}

	raw, err := c.Clone(ctx, orig)
	if err != nil {
		t.Fatalf("Clone failed: %v", err)
	}
	got := raw.(*Person)

	if got.NilPtr != nil {
		t.Error("expected nil NilPtr")
	}
	if got.Tags != nil {
		t.Error("expected nil Tags")
	}
	if got.Meta != nil {
		t.Error("expected nil Meta")
	}
}

func TestReflectCloner_NilEntity(t *testing.T) {
	c := cloner.NewReflect()
	_, err := c.Clone(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil entity")
	}
}

func BenchmarkClone_ReflectCloner_20Fields(b *testing.B) {
	type Big struct {
		F01, F02, F03, F04, F05 string
		F06, F07, F08, F09, F10 int
		F11, F12, F13, F14, F15 float64
		F16, F17, F18, F19, F20 bool
	}

	c := cloner.NewReflect()
	ctx := context.Background()
	entity := &Big{
		F01: "a", F02: "b", F03: "c", F04: "d", F05: "e",
		F06: 1, F07: 2, F08: 3, F09: 4, F10: 5,
		F11: 1.1, F12: 2.2, F13: 3.3, F14: 4.4, F15: 5.5,
		F16: true, F17: false, F18: true, F19: false, F20: true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Clone(ctx, entity)
	}
}
