package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time interface check.
var _ port.VersionReaderPort = (*Reader)(nil)

// Reader implements VersionReaderPort for MySQL.
type Reader struct {
	db MySQLDB
}

// NewReader returns a Reader backed by the given MySQL connection.
func NewReader(db MySQLDB) *Reader {
	return &Reader{db: db}
}

const selectCols = `id, entity_type, entity_id, version, strategy,
    schema_version, data, content_type, previous_hash, hash,
    metadata, created_at`

// GetByVersion retrieves a specific version of an entity.
func (r *Reader) GetByVersion(ctx context.Context, entityType, entityID string, version int64) (domain.VersionRecord, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+selectCols+` FROM audit_versions
		 WHERE entity_type = ? AND entity_id = ? AND version = ?`,
		entityType, entityID, version,
	)
	return scanRow(row)
}

// GetLatest retrieves the most recent version of an entity.
func (r *Reader) GetLatest(ctx context.Context, entityType, entityID string) (domain.VersionRecord, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+selectCols+` FROM audit_versions
		 WHERE entity_type = ? AND entity_id = ?
		 ORDER BY version DESC LIMIT 1`,
		entityType, entityID,
	)
	return scanRow(row)
}

// GetAtTime retrieves the version of an entity that was current at time t.
func (r *Reader) GetAtTime(ctx context.Context, entityType, entityID string, t time.Time) (domain.VersionRecord, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+selectCols+` FROM audit_versions
		 WHERE entity_type = ? AND entity_id = ? AND created_at <= ?
		 ORDER BY created_at DESC, version DESC LIMIT 1`,
		entityType, entityID, t,
	)
	return scanRow(row)
}

// Pre-built query templates keyed by sort direction.
var listQueryByOrder = map[bool]string{
	true: `SELECT ` + selectCols + ` FROM audit_versions
			 WHERE entity_type = ? AND entity_id = ?
			 ORDER BY version ASC LIMIT ? OFFSET ?`,
	false: `SELECT ` + selectCols + ` FROM audit_versions
			 WHERE entity_type = ? AND entity_id = ?
			 ORDER BY version DESC LIMIT ? OFFSET ?`,
}

// ListVersions returns a paginated list of version records for an entity.
func (r *Reader) ListVersions(ctx context.Context, entityType, entityID string, opts ...port.ListOption) ([]domain.VersionRecord, error) {
	o := port.ApplyListOptions(opts...)

	rows, err := r.db.QueryContext(ctx, listQueryByOrder[o.OrderAsc],
		entityType, entityID, o.Limit, o.Offset,
	)
	if err != nil {
		return nil, fmt.Errorf("mysql: list versions: %w", err)
	}
	defer rows.Close()

	var records []domain.VersionRecord
	for rows.Next() {
		rec, err := scanRows(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql: rows iteration: %w", err)
	}
	return records, nil
}

// scanner is the common interface between MySQLRow and MySQLRows.
type scanner interface {
	Scan(dest ...any) error
}

// scan reads a row into a VersionRecord from any scanner.
func scan(s scanner) (domain.VersionRecord, error) {
	var rec domain.VersionRecord
	var strategyInt int16
	var metaJSON []byte

	err := s.Scan(
		&rec.ID,
		&rec.EntityType,
		&rec.EntityID,
		&rec.Version,
		&strategyInt,
		&rec.SchemaVersion,
		&rec.Data,
		&rec.ContentType,
		&rec.PreviousHash,
		&rec.Hash,
		&metaJSON,
		&rec.CreatedAt,
	)
	if err != nil {
		if isNotFoundErr(err) {
			return domain.VersionRecord{}, domain.ErrNotFound
		}
		return domain.VersionRecord{}, fmt.Errorf("mysql: scan: %w", err)
	}

	rec.Strategy = domain.Strategy(strategyInt)

	if err := json.Unmarshal(metaJSON, &rec.Metadata); err != nil {
		return domain.VersionRecord{}, fmt.Errorf("mysql: unmarshal metadata: %w", err)
	}

	return rec, nil
}

// scanRow scans a single MySQLRow into a VersionRecord.
func scanRow(row MySQLRow) (domain.VersionRecord, error) {
	return scan(row)
}

// scanRows scans the current row from MySQLRows into a VersionRecord.
func scanRows(rows MySQLRows) (domain.VersionRecord, error) {
	return scan(rows)
}

// errNoRowsMsg is the error message produced by database/sql.ErrNoRows.
// We match by string instead of importing database/sql to keep this
// sub-module free of driver dependencies — callers supply the concrete
// driver via the MySQLDB interface.
const errNoRowsMsg = "sql: no rows in result set"

// isNotFoundErr checks for sql.ErrNoRows or similar "no rows" indicators.
func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return msg == errNoRowsMsg || msg == "no rows"
}
