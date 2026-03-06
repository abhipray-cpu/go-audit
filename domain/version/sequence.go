package version

import (
	"context"
	"errors"
	"fmt"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// NextVersion queries the latest version for the given entity and returns
// the next monotonically increasing version number. If no previous version
// exists, it returns 1.
//
// This is the single source of truth for version sequence generation.
// Uniqueness is ultimately enforced by the database's UNIQUE constraint
// on (entity_type, entity_id, version).
func NextVersion(ctx context.Context, reader port.VersionReaderPort, entityType, entityID string) (int64, error) {
	latest, err := reader.GetLatest(ctx, entityType, entityID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return 1, nil
		}
		return 0, fmt.Errorf("version: query latest: %w", err)
	}
	return latest.Version + 1, nil
}
