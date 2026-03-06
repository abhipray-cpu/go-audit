package port

import (
	"context"

	"github.com/abhipray-cpu/go-audit/domain"
)

// VersionWriterPort persists version records to the primary storage backend.
type VersionWriterPort interface {
	// Save persists a single version record.
	Save(ctx context.Context, record domain.VersionRecord) error

	// Update overwrites the data (snapshot) of an existing version record,
	// identified by (entity_type, entity_id, version). It is used by
	// redaction workflows that must modify a previously saved version
	// in-place without creating a new version number.
	Update(ctx context.Context, record domain.VersionRecord) error

	// SaveInTx persists a version record within a caller-managed transaction.
	// The record is only visible after the transaction commits.
	SaveInTx(ctx context.Context, tx Transaction, record domain.VersionRecord) error
}
