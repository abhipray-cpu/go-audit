package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time interface check.
var _ port.VersionWriterPort = (*Writer)(nil)

// Writer implements VersionWriterPort for MySQL.
type Writer struct {
	db MySQLDB
}

// NewWriter returns a Writer backed by the given MySQL connection.
func NewWriter(db MySQLDB) *Writer {
	return &Writer{db: db}
}

const insertSQL = `INSERT INTO audit_versions
    (id, entity_type, entity_id, version, strategy,
     schema_version, data, content_type,
     previous_hash, hash, metadata, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const updateSQL = `UPDATE audit_versions
    SET data          = ?,
        hash          = ?,
        previous_hash = ?,
        metadata      = ?,
        strategy      = ?
  WHERE entity_type = ?
    AND entity_id   = ?
    AND version     = ?`

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
		return fmt.Errorf("mysql: marshal metadata: %w", err)
	}

	result, err := w.db.ExecContext(ctx, updateSQL,
		record.Data,
		record.Hash,
		record.PreviousHash,
		string(metaJSON),
		int16(record.Strategy),
		record.EntityType,
		record.EntityID,
		record.Version,
	)
	if err != nil {
		return fmt.Errorf("mysql: update version: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql: rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: entity_type=%s entity_id=%s version=%d",
			domain.ErrNotFound, record.EntityType, record.EntityID, record.Version)
	}
	return nil
}

// SaveInTx persists a version record within a caller-managed transaction.
// The tx argument must satisfy MySQLDB (e.g., *sql.Tx).
func (w *Writer) SaveInTx(ctx context.Context, tx port.Transaction, record domain.VersionRecord) error {
	mtx, ok := tx.(MySQLDB)
	if !ok {
		return fmt.Errorf("mysql: expected MySQLDB, got %T", tx)
	}
	return w.execInsert(ctx, mtx, record)
}

func (w *Writer) execInsert(ctx context.Context, db MySQLDB, record domain.VersionRecord) error {
	metaJSON, err := json.Marshal(record.Metadata)
	if err != nil {
		return fmt.Errorf("mysql: marshal metadata: %w", err)
	}

	_, err = db.ExecContext(ctx, insertSQL,
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
		string(metaJSON),
		record.CreatedAt,
	)
	if err != nil {
		// MySQL duplicate key: Error 1062 or "Duplicate entry" message.
		if isDuplicateKeyErr(err) {
			return fmt.Errorf("%w: entity_type=%s entity_id=%s version=%d",
				domain.ErrDuplicateVersion, record.EntityType, record.EntityID, record.Version)
		}
		return fmt.Errorf("mysql: insert version: %w", err)
	}
	return nil
}

// isDuplicateKeyErr detects MySQL duplicate key violations.
func isDuplicateKeyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Duplicate entry") ||
		strings.Contains(msg, "1062") ||
		strings.Contains(msg, "UNIQUE constraint")
}
