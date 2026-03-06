package port

import (
	"context"

	"github.com/abhipray-cpu/go-audit/domain"
)

// CachePort provides in-memory caching for frequently accessed version records.
type CachePort interface {
	// Get retrieves a cached version record. Returns the record and true if found,
	// or a zero value and false if not cached.
	Get(ctx context.Context, entityType, entityID string, version int64) (domain.VersionRecord, bool)

	// Put stores a version record in the cache.
	Put(ctx context.Context, record domain.VersionRecord)

	// Invalidate removes all cached entries for the given entity.
	Invalidate(ctx context.Context, entityType, entityID string)
}
