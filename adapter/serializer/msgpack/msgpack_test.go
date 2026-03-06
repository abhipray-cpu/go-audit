package msgpack

import (
	"context"
	"testing"
)

type testEntity struct {
	ID    string `msgpack:"id"`
	Name  string `msgpack:"name"`
	Email string `msgpack:"email"`
	Age   int    `msgpack:"age"`
}

func TestMsgPack_RoundTrip(t *testing.T) {
	s := New()

	original := &testEntity{
		ID:    "u1",
		Name:  "Alice",
		Email: "alice@example.com",
		Age:   30,
	}

	data, err := s.Marshal(context.Background(), original)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("expected non-empty data")
	}

	// MsgPack should be smaller than JSON for struct data.
	t.Logf("msgpack size: %d bytes", len(data))

	var decoded testEntity
	if err := s.Unmarshal(context.Background(), data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Name != original.Name {
		t.Errorf("Name: got %q, want %q", decoded.Name, original.Name)
	}
	if decoded.Email != original.Email {
		t.Errorf("Email: got %q, want %q", decoded.Email, original.Email)
	}
	if decoded.Age != original.Age {
		t.Errorf("Age: got %d, want %d", decoded.Age, original.Age)
	}
}

func TestMsgPack_ContentType(t *testing.T) {
	s := New()
	if ct := s.ContentType(); ct != "application/msgpack" {
		t.Errorf("ContentType() = %q, want %q", ct, "application/msgpack")
	}
}
