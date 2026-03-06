// Package noop provides a zero-overhead [port.WALPort] implementation.
//
// NoopWAL silently discards all writes and returns empty replays.
// It is the default WAL adapter — use it when crash recovery is not required.
//
// Performance: all operations complete in O(1) with zero allocations.
package noop

import (
	"context"

	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time check.
var _ port.WALPort = (*WAL)(nil)

// WAL is a no-operation Write-Ahead Log. All methods are safe for concurrent use.
type WAL struct{}

// New returns a new no-operation WAL.
func New() *WAL { return &WAL{} }

// Append is a no-op; it returns nil immediately.
func (w *WAL) Append(_ context.Context, _ port.WALEntry) error { return nil }

// Ack is a no-op; it returns nil immediately.
func (w *WAL) Ack(_ context.Context, _ string) error { return nil }

// Replay always returns an empty slice and nil error.
func (w *WAL) Replay(_ context.Context) ([]port.WALEntry, error) { return nil, nil }
