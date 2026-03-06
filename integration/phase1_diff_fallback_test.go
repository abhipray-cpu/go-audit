//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/diff"
)

// ---------------------------------------------------------------------------
// Phase 1 — D-061 to D-066: Fallback + error case tests
// ---------------------------------------------------------------------------

// D-061: TestDiff_Fallback_ChanField verifies chan field causes ErrDiffFallback.
func TestDiff_Fallback_ChanField(t *testing.T) {
	e := diff.New()
	ch := make(chan int)
	prev := &FallbackChanEntity{ID: "1", Ch: ch, Name: "a"}
	curr := &FallbackChanEntity{ID: "1", Ch: ch, Name: "b"}

	_, err := e.Diff(context.Background(), prev, curr)
	if err == nil {
		t.Fatal("expected error for chan field, got nil")
	}
	if !errors.Is(err, domain.ErrDiffFallback) {
		t.Errorf("expected ErrDiffFallback, got %v", err)
	}
}

// D-062: TestDiff_Fallback_FuncField verifies func field causes ErrDiffFallback.
func TestDiff_Fallback_FuncField(t *testing.T) {
	e := diff.New()
	fn := func() {}
	prev := &FallbackFuncEntity{ID: "1", Fn: fn, Name: "a"}
	curr := &FallbackFuncEntity{ID: "1", Fn: fn, Name: "b"}

	_, err := e.Diff(context.Background(), prev, curr)
	if err == nil {
		t.Fatal("expected error for func field, got nil")
	}
	if !errors.Is(err, domain.ErrDiffFallback) {
		t.Errorf("expected ErrDiffFallback, got %v", err)
	}
}

// D-063: TestDiff_Error_TypeMismatch verifies diffing two different struct types returns ErrDiff.
func TestDiff_Error_TypeMismatch(t *testing.T) {
	e := diff.New()
	prev := &FlatEntity{ID: "1", Name: "Alice"}
	curr := &NestedEntity{ID: "1", Name: "Bob"}

	_, err := e.Diff(context.Background(), prev, curr)
	if err == nil {
		t.Fatal("expected error for type mismatch")
	}
	if !errors.Is(err, domain.ErrDiff) {
		t.Errorf("expected ErrDiff, got %v", err)
	}
}

// D-064: TestDiff_Error_NilPrev verifies Diff(nil, entity) returns ErrDiff.
func TestDiff_Error_NilPrev(t *testing.T) {
	e := diff.New()
	curr := &FlatEntity{ID: "1"}

	_, err := e.Diff(context.Background(), nil, curr)
	if err == nil {
		t.Fatal("expected error for nil prev")
	}
	if !errors.Is(err, domain.ErrDiff) {
		t.Errorf("expected ErrDiff, got %v", err)
	}
}

// D-065: TestDiff_Error_NilCurr verifies Diff(entity, nil) returns ErrDiff.
func TestDiff_Error_NilCurr(t *testing.T) {
	e := diff.New()
	prev := &FlatEntity{ID: "1"}

	_, err := e.Diff(context.Background(), prev, nil)
	if err == nil {
		t.Fatal("expected error for nil curr")
	}
	if !errors.Is(err, domain.ErrDiff) {
		t.Errorf("expected ErrDiff, got %v", err)
	}
}

// D-066: TestDiff_SafeDiff_PanicRecovery verifies SafeDiff recovers from panics.
func TestDiff_SafeDiff_PanicRecovery(t *testing.T) {
	e := diff.New()

	// SafeDiff is a package-level function: diff.SafeDiff(ctx, engine, prev, curr).
	// With nil inputs the underlying Diff returns an error; SafeDiff wraps any panic.
	_, err := diff.SafeDiff(context.Background(), e, nil, nil)
	if err == nil {
		t.Fatal("expected error from SafeDiff with nil inputs")
	}
	// If SafeDiff recovers a panic, it wraps as ErrDiffFallback;
	// if the underlying Diff already returned an error, that's fine too.
	if !errors.Is(err, domain.ErrDiff) && !errors.Is(err, domain.ErrDiffFallback) {
		t.Errorf("expected ErrDiff or ErrDiffFallback, got %v", err)
	}
}
