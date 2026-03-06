//go:build integration

package integration

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain/hooks"
)

// ---------------------------------------------------------------------------
// Phase 2 — H-001 to H-006: Authorization Hooks
// ---------------------------------------------------------------------------

// H-001: TestHooks_BeforeWrite_Allowed verifies a passing hook doesn't block writes.
func TestHooks_BeforeWrite_Allowed(t *testing.T) {
	reg := hooks.New()
	reg.OnBeforeWrite("allow-all", func(ctx context.Context, entityType, entityID string) error {
		return nil
	})

	if err := reg.BeforeWrite(context.Background(), "user", "123"); err != nil {
		t.Fatalf("BeforeWrite should pass: %v", err)
	}
}

// H-002: TestHooks_BeforeWrite_Rejected verifies a rejecting hook returns error.
func TestHooks_BeforeWrite_Rejected(t *testing.T) {
	reg := hooks.New()
	reg.OnBeforeWrite("deny-all", func(ctx context.Context, entityType, entityID string) error {
		return errors.New("access denied")
	})

	err := reg.BeforeWrite(context.Background(), "user", "123")
	if err == nil {
		t.Fatal("expected error from rejecting hook")
	}
	if err.Error() != "access denied" {
		t.Fatalf("expected 'access denied', got: %v", err)
	}
}

// H-003: TestHooks_BeforeRead_Allowed verifies read hooks allow reads.
func TestHooks_BeforeRead_Allowed(t *testing.T) {
	reg := hooks.New()
	reg.OnBeforeRead("allow-all", func(ctx context.Context, entityType, entityID string) error {
		return nil
	})

	if err := reg.BeforeRead(context.Background(), "user", "123"); err != nil {
		t.Fatalf("BeforeRead should pass: %v", err)
	}
}

// H-004: TestHooks_BeforeRead_Rejected verifies read hooks can deny reads.
func TestHooks_BeforeRead_Rejected(t *testing.T) {
	reg := hooks.New()
	reg.OnBeforeRead("deny-reads", func(ctx context.Context, entityType, entityID string) error {
		return errors.New("read denied")
	})

	err := reg.BeforeRead(context.Background(), "user", "123")
	if err == nil {
		t.Fatal("expected error from rejecting read hook")
	}
}

// H-005: TestHooks_PanicRecovery verifies panicking hooks are recovered.
func TestHooks_PanicRecovery(t *testing.T) {
	reg := hooks.New()
	reg.OnBeforeWrite("panic-hook", func(ctx context.Context, entityType, entityID string) error {
		panic("boom")
	})

	err := reg.BeforeWrite(context.Background(), "user", "123")
	if err == nil {
		t.Fatal("expected error from panic recovery")
	}
	if !containsString(err.Error(), "panicked") {
		t.Fatalf("expected panic recovery error, got: %v", err)
	}
}

// H-006: TestHooks_ExecutionOrder verifies hooks execute in registration order.
func TestHooks_ExecutionOrder(t *testing.T) {
	reg := hooks.New()
	var mu sync.Mutex
	var order []string

	for _, name := range []string{"first", "second", "third"} {
		n := name
		reg.OnBeforeWrite(n, func(ctx context.Context, entityType, entityID string) error {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, n)
			return nil
		})
	}

	if err := reg.BeforeWrite(context.Background(), "user", "123"); err != nil {
		t.Fatalf("BeforeWrite: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 {
		t.Fatalf("expected 3 hooks, got %d", len(order))
	}
	if order[0] != "first" || order[1] != "second" || order[2] != "third" {
		t.Fatalf("expected [first second third], got %v", order)
	}
}

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && findSubstring(s, sub) >= 0
}

func findSubstring(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
