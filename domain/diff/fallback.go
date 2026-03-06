package diff

import (
	"context"
	"fmt"

	"github.com/abhipray-cpu/go-audit/domain"
)

// SafeDiff wraps Engine.Diff with panic recovery. If the diff walk panics
// (e.g. due to a custom type's method panicking during reflection), the
// panic is caught and converted into a domain.ErrDiffFallback error. This
// allows callers to fall back to a full-snapshot strategy instead of
// crashing the process.
//
// Usage:
//
//	delta, err := diff.SafeDiff(ctx, engine, prev, curr)
//	if errors.Is(err, domain.ErrDiffFallback) {
//	    // store full snapshot instead of delta
//	}
func SafeDiff(ctx context.Context, e *Engine, prev, curr any) (delta domain.Delta, err error) {
	defer func() {
		if r := recover(); r != nil {
			delta = domain.Delta{}
			err = fmt.Errorf("%w: panic recovered during diff: %v", domain.ErrDiffFallback, r)
		}
	}()

	return e.Diff(ctx, prev, curr)
}
