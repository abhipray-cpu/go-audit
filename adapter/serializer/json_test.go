package serializer_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/abhipray-cpu/go-audit/adapter/serializer"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time interface check (test side).
var _ port.SerializerPort = (*serializer.JSONSerializer)(nil)

type sample struct {
	Name  string `json:"name"`
	Age   int    `json:"age"`
	Email string `json:"email,omitempty"`
}

func TestJSONSerializer_RoundTrip(t *testing.T) {
	s := serializer.NewJSON()
	ctx := context.Background()

	orig := sample{Name: "Alice", Age: 30, Email: "alice@example.com"}

	data, err := s.Marshal(ctx, orig)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var got sample
	if err := s.Unmarshal(ctx, data, &got); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if !reflect.DeepEqual(orig, got) {
		t.Errorf("round-trip mismatch:\n  orig: %+v\n  got:  %+v", orig, got)
	}
}

func TestJSONSerializer_ContentType(t *testing.T) {
	s := serializer.NewJSON()
	if ct := s.ContentType(); ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
}

func TestJSONSerializer_NilInput(t *testing.T) {
	s := serializer.NewJSON()
	ctx := context.Background()

	// Marshal nil → error, no panic.
	_, err := s.Marshal(ctx, nil)
	if err == nil {
		t.Fatal("expected error when marshalling nil")
	}

	// Unmarshal with nil target → error, no panic.
	err = s.Unmarshal(ctx, []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected error when target is nil")
	}

	// Unmarshal with empty data → error, no panic.
	var v sample
	err = s.Unmarshal(ctx, nil, &v)
	if err == nil {
		t.Fatal("expected error when data is nil")
	}
}
