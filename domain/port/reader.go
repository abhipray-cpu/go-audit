package port

import (
	"context"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
)

// VersionReaderPort reads version records from the primary storage backend.
type VersionReaderPort interface {
	// GetByVersion retrieves a specific version of an entity.
	GetByVersion(ctx context.Context, entityType, entityID string, version int64) (domain.VersionRecord, error)

	// GetLatest retrieves the most recent version of an entity.
	GetLatest(ctx context.Context, entityType, entityID string) (domain.VersionRecord, error)

	// GetAtTime retrieves the version of an entity that was current at the given time.
	GetAtTime(ctx context.Context, entityType, entityID string, t time.Time) (domain.VersionRecord, error)

	// ListVersions returns a paginated list of version records for an entity.
	ListVersions(ctx context.Context, entityType, entityID string, opts ...ListOption) ([]domain.VersionRecord, error)
}
