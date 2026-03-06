package port

import (
	"context"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
)

// QueryPort is the inbound port for reading and querying version history.
// Host applications call these methods to retrieve audit trail data.
type QueryPort interface {
	// GetVersion retrieves a specific version of an entity by its version number.
	GetVersion(ctx context.Context, entityType, entityID string, version int64) (domain.VersionRecord, error)

	// GetLatest retrieves the most recent version of an entity.
	GetLatest(ctx context.Context, entityType, entityID string) (domain.VersionRecord, error)

	// GetAtTime retrieves the version of an entity that was current at the given point in time.
	GetAtTime(ctx context.Context, entityType, entityID string, t time.Time) (domain.VersionRecord, error)

	// ListVersions returns a paginated list of version records for an entity.
	ListVersions(ctx context.Context, entityType, entityID string, opts ...ListOption) ([]domain.VersionRecord, error)

	// Compare computes the diff between two versions of the same entity.
	Compare(ctx context.Context, entityType, entityID string, v1, v2 int64) (domain.Delta, error)

	// FindChanges returns the history of changes to a specific field across versions.
	FindChanges(ctx context.Context, entityType, entityID, fieldName string, opts ...ListOption) ([]domain.FieldChange, error)

	// FindByActor returns all version records created by the specified actor.
	FindByActor(ctx context.Context, actorID string, opts ...ListOption) ([]domain.VersionRecord, error)

	// GetBulkAtTime retrieves the version that was current at the given time
	// for each of the specified entity IDs. The returned map is keyed by entity ID.
	GetBulkAtTime(ctx context.Context, entityType string, entityIDs []string, t time.Time) (map[string]domain.VersionRecord, error)
}
