package audittest

import (
	"context"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain/port"
)

type helperUser struct {
	ID    string `version:"id"`
	Name  string `version:"tracked"`
	Email string `version:"tracked"`
}

func TestNewAuditor(t *testing.T) {
	a, store := NewAuditor(t)

	if a == nil {
		t.Fatal("NewAuditor returned nil Auditor")
	}
	if store == nil {
		t.Fatal("NewAuditor returned nil store")
	}
	if store.Len() != 0 {
		t.Errorf("store.Len() = %d, want 0", store.Len())
	}
}

func TestNewAuditor_FullWorkflow(t *testing.T) {
	a, store := NewAuditor(t)

	if err := a.Register(&helperUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	user := &helperUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	p1, err := a.Version(context.Background(), user, port.WithSync())
	if err != nil {
		t.Fatalf("Version() v1 error: %v", err)
	}
	r1, err := p1.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() v1 error: %v", err)
	}
	if r1.Version != 1 {
		t.Errorf("expected v1, got %d", r1.Version)
	}

	// Mutate and create v2.
	user.Name = "Bob"
	p2, err := a.Version(context.Background(), user, port.WithSync())
	if err != nil {
		t.Fatalf("Version() v2 error: %v", err)
	}
	r2, err := p2.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() v2 error: %v", err)
	}
	if r2.Version != 2 {
		t.Errorf("expected v2, got %d", r2.Version)
	}

	// Verify via store.
	if store.Len() != 2 {
		t.Errorf("store.Len() = %d, want 2", store.Len())
	}

	recs := store.RecordsFor("helperuser", "u1")
	if len(recs) != 2 {
		t.Errorf("RecordsFor = %d, want 2", len(recs))
	}
}
