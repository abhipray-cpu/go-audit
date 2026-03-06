package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Compile-time interface check.
var _ port.VersionWriterPort = (*Writer)(nil)

// Writer implements VersionWriterPort for PostgreSQL.
type Writer struct {
	db DB
}

// NewWriter returns a Writer backed by the given database handle.
func NewWriter(db DB) *Writer {
	return &Writer{db: db}
}

const insertSQL = `
INSERT INTO audit_versions (
    id, entity_type, entity_id, version, strategy,
    schema_version, data, content_type,
    previous_hash, hash, metadata, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`

const updateSQL = `
UPDATE audit_versions
   SET data          = $1,
       hash          = $2,
       previous_hash = $3,
       metadata      = $4,
       strategy      = $5
 WHERE entity_type = $6
   AND entity_id   = $7
   AND version     = $8`

// Save persists a version record using the Writer's database handle.
func (w *Writer) Save(ctx context.Context, record domain.VersionRecord) error {
	return w.execInsert(ctx, w.db, record)
}

// Update overwrites the data/hash/metadata/strategy of an existing version
// record. It is used by redaction and compaction to modify a previously saved
// version in-place.
func (w *Writer) Update(ctx context.Context, record domain.VersionRecord) error {
	metaJSON, err := json.Marshal(record.Metadata)
	if err != nil {
		return fmt.Errorf("postgres: marshal metadata: %w", err)
	}

	tag, err := w.db.Exec(ctx, updateSQL,
		record.Data,
		record.Hash,
		record.PreviousHash,
		metaJSON,
		int16(record.Strategy),
		record.EntityType,
		record.EntityID,
		record.Version,
	)
	if err != nil {
		return fmt.Errorf("postgres: update version: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: entity_type=%s entity_id=%s version=%d",
			domain.ErrNotFound, record.EntityType, record.EntityID, record.Version)
	}
	return nil
}

// SaveInTx persists a version record within a caller-managed transaction.
// The tx argument must be a *pgx.Tx.
func (w *Writer) SaveInTx(ctx context.Context, tx port.Transaction, record domain.VersionRecord) error {
	ptx, ok := tx.(pgx.Tx)
	if !ok {
		return fmt.Errorf("postgres: expected pgx.Tx, got %T", tx)
	}
	return w.execInsert(ctx, ptx, record)
}

// execable is the minimal interface for executing a parameterized INSERT.
type execable interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func (w *Writer) execInsert(ctx context.Context, e execable, record domain.VersionRecord) error {
	metaJSON, err := json.Marshal(record.Metadata)
	if err != nil {
		return fmt.Errorf("postgres: marshal metadata: %w", err)
	}

	_, err = e.Exec(ctx, insertSQL,
		record.ID,
		record.EntityType,
		record.EntityID,
		record.Version,
		int16(record.Strategy),
		record.SchemaVersion,
		record.Data,
		record.ContentType,
		record.PreviousHash,
		record.Hash,
		metaJSON,
		record.CreatedAt,
	)
	if err != nil {
		// Check for unique constraint violation → ErrDuplicateVersion.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("%w: entity_type=%s entity_id=%s version=%d",
				domain.ErrDuplicateVersion, record.EntityType, record.EntityID, record.Version)
		}
		return fmt.Errorf("postgres: insert version: %w", err)
	}
	return nil
}
