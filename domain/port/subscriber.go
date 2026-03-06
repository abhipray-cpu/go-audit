package port

import (
	"context"

	"github.com/abhipray-cpu/go-audit/domain"
)

// SubscriberPort receives notifications after a version is successfully persisted.
// Subscribers are called asynchronously with failure isolation.
type SubscriberPort interface {
	// OnVersion is called after a version record has been persisted.
	OnVersion(ctx context.Context, record domain.VersionRecord) error
}
