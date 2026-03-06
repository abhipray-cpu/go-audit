package port

import (
	"context"

	"github.com/abhipray-cpu/go-audit/domain"
)

// VersioningPort is the primary inbound port for creating audit versions.
// Host applications call these methods to record entity changes.
type VersioningPort interface {
	// Version creates a new version of the given entity asynchronously.
	// The entity is deep-copied immediately; the caller may mutate it
	// after this call returns. Options control sync mode and strategy.
	Version(ctx context.Context, entity any, opts ...VersionOption) (*domain.PendingVersion, error)

	// VersionInTx creates a new version within a caller-managed database
	// transaction. The version record is only visible after the transaction
	// commits. This call is always synchronous.
	VersionInTx(ctx context.Context, tx Transaction, entity any) (domain.VersionRecord, error)

	// Register registers an entity type for auditing. The entity's struct
	// type is inspected for field tags and configuration. Must be called
	// before any Version() calls for this entity type.
	Register(entity any) error

	// Shutdown gracefully drains the background worker pool and releases
	// resources. The provided context controls the maximum time to wait
	// for in-flight versions to complete.
	Shutdown(ctx context.Context) error
}
