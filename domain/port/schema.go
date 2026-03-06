package port

import (
	"context"
)

// SchemaManagerPort manages database schema migrations for the audit tables.
// This is an outbound port — the domain tells adapters to migrate.
type SchemaManagerPort interface {
	// Migrate applies all pending schema migrations idempotently.
	Migrate(ctx context.Context) error

	// CurrentVersion returns the current schema version number.
	CurrentVersion(ctx context.Context) (int, error)
}
