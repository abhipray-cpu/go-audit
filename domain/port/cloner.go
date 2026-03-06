package port

import (
	"context"
)

// ClonerPort creates independent deep copies of entities.
// The clone is made before enqueuing to the background worker so that
// subsequent mutations by the caller do not affect the audit record.
type ClonerPort interface {
	// Clone returns a deep copy of the given entity.
	Clone(ctx context.Context, entity any) (any, error)
}
