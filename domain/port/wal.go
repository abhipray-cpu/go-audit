package port

import (
	"context"
)

// WALPort provides a Write-Ahead Log for crash recovery.
// Entries are appended before enqueue and acknowledged after successful persistence.
type WALPort interface {
	// Append writes a version record to the WAL before it enters the worker queue.
	Append(ctx context.Context, entry WALEntry) error

	// Ack acknowledges that a WAL entry has been successfully persisted,
	// removing it from the replay set.
	Ack(ctx context.Context, entryID string) error

	// Replay returns all un-acknowledged WAL entries for crash recovery.
	// Called at startup to re-process any in-flight versions.
	Replay(ctx context.Context) ([]WALEntry, error)
}
