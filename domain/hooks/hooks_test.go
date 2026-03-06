package hooks

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestBeforeWriteHook_Denied(t *testing.T) {
	reg := New()
	denied := errors.New("access denied")

	reg.OnBeforeWrite("authz", func(_ context.Context, entityType, entityID string) error {
		if entityType == "secret" {
			return denied
		}
		return nil
	})

	// Allowed entity type.
	if err := reg.BeforeWrite(context.Background(), "user", "u1"); err != nil {
		t.Fatalf("expected no error for user, got %v", err)
	}

	// Denied entity type.
	if err := reg.BeforeWrite(context.Background(), "secret", "s1"); err == nil {
		t.Fatal("expected error for secret, got nil")
	} else if !errors.Is(err, denied) {
		t.Fatalf("expected denied error, got %v", err)
	}
}

func TestBeforeReadHook_Denied(t *testing.T) {
	reg := New()
	denied := errors.New("read not allowed")

	reg.OnBeforeRead("authz", func(_ context.Context, _, entityID string) error {
		if entityID == "classified" {
			return denied
		}
		return nil
	})

	// Allowed read.
	if err := reg.BeforeRead(context.Background(), "user", "u1"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Denied read.
	if err := reg.BeforeRead(context.Background(), "user", "classified"); err == nil {
		t.Fatal("expected error for classified, got nil")
	}
}

func TestHook_PanicRecovery(t *testing.T) {
	reg := New()

	reg.OnBeforeWrite("crasher", func(_ context.Context, _, _ string) error {
		panic("boom")
	})

	err := reg.BeforeWrite(context.Background(), "user", "u1")
	if err == nil {
		t.Fatal("expected error from panicking hook, got nil")
	}
	t.Logf("Recovered: %v", err)
}

func TestHook_ExecutionOrder(t *testing.T) {
	reg := New()

	var order []int

	reg.OnBeforeWrite("first", func(_ context.Context, _, _ string) error {
		order = append(order, 1)
		return nil
	})
	reg.OnBeforeWrite("second", func(_ context.Context, _, _ string) error {
		order = append(order, 2)
		return nil
	})
	reg.OnBeforeWrite("third", func(_ context.Context, _, _ string) error {
		order = append(order, 3)
		return nil
	})

	if err := reg.BeforeWrite(context.Background(), "user", "u1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("expected 3 hooks executed, got %d", len(order))
	}
	for i, v := range order {
		if v != i+1 {
			t.Errorf("hook %d: expected %d, got %d", i, i+1, v)
		}
	}
}

func TestHook_EarlyReturn(t *testing.T) {
	reg := New()
	var secondCalled atomic.Bool

	reg.OnBeforeWrite("blocker", func(_ context.Context, _, _ string) error {
		return errors.New("blocked")
	})
	reg.OnBeforeWrite("after", func(_ context.Context, _, _ string) error {
		secondCalled.Store(true)
		return nil
	})

	if err := reg.BeforeWrite(context.Background(), "user", "u1"); err == nil {
		t.Fatal("expected error, got nil")
	}
	if secondCalled.Load() {
		t.Error("second hook should not have been called after first returned error")
	}
}
