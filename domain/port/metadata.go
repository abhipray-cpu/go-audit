package port

import (
	"context"

	"github.com/abhipray-cpu/go-audit/domain"
)

// MetadataExtractorPort extracts audit metadata from the request context.
// Adapters pull actor, reason, correlation ID, and custom fields from
// context values, HTTP headers, gRPC metadata, etc.
type MetadataExtractorPort interface {
	// Extract pulls audit metadata from the given context.
	Extract(ctx context.Context) domain.VersionMetadata
}
