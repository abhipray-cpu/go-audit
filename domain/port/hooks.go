package port

import (
	"context"
)

// HooksPort executes lifecycle hooks before reads and writes.
// Hooks run in registration order with panic recovery. If any hook
// returns an error, the operation is rejected.
type HooksPort interface {
	// BeforeWrite is called before a version record is persisted.
	// Return an error to reject the write.
	BeforeWrite(ctx context.Context, entityType, entityID string) error

	// BeforeRead is called before a version record is read.
	// Return an error to reject the read.
	BeforeRead(ctx context.Context, entityType, entityID string) error
}
