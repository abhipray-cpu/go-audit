package port

import (
	"context"

	"github.com/abhipray-cpu/go-audit/domain"
)

// DifferPort computes and applies structural diffs between entity versions.
type DifferPort interface {
	// Diff compares prev and curr, returning the set of field-level changes.
	Diff(ctx context.Context, prev, curr any) (domain.Delta, error)

	// Apply reconstructs the current entity state by applying a delta to a previous state.
	Apply(ctx context.Context, prev any, delta domain.Delta) (any, error)
}
