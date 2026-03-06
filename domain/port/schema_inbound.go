package port

import "context"

// SchemaPort is the inbound port for schema management operations.
// Host applications call these methods to register migrations and
// verify the integrity of the audit data store.
type SchemaPort interface {
	// RegisterMigration registers a migration function that transforms
	// entity data from one schema version to another. Migrations are
	// applied lazily on read. The fromVersion and toVersion must be
	// consecutive (no gaps allowed).
	RegisterMigration(entityType string, fromVersion, toVersion int, fn MigrationFunc) error

	// VerifyIntegrity checks the audit trail for consistency errors,
	// including hash chain validation and schema version gaps.
	VerifyIntegrity(ctx context.Context, entityType, entityID string) error
}
