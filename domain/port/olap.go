package port

import (
	"context"

	"github.com/abhipray-cpu/go-audit/domain"
)

// OLAPPort provides batch write and analytical query access to the OLAP backend
// (e.g., ClickHouse).
type OLAPPort interface {
	// BatchInsert writes a batch of version records to the OLAP store.
	BatchInsert(ctx context.Context, records []domain.VersionRecord) error

	// Query executes an analytical query and returns matching version records.
	Query(ctx context.Context, entityType string, opts ...ListOption) ([]domain.VersionRecord, error)
}
