package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
	"github.com/jackc/pgx/v5"
)

// Compile-time interface check.
var _ port.VersionReaderPort = (*Reader)(nil)

// Reader implements VersionReaderPort for PostgreSQL.
type Reader struct {
	db DB
}

// NewReader returns a Reader backed by the given database handle.
func NewReader(db DB) *Reader {
	return &Reader{db: db}
}

// Query adds the Query method to the DB interface for list operations.
// Both *pgx.Conn and *pgxpool.Pool satisfy this extended interface.
type queryDB interface {
	DB
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

const selectCols = `id, entity_type, entity_id, version, strategy,
    schema_version, data, content_type, previous_hash, hash,
    metadata, created_at`

// GetByVersion retrieves a specific version of an entity.
func (r *Reader) GetByVersion(ctx context.Context, entityType, entityID string, version int64) (domain.VersionRecord, error) {
	row := r.db.QueryRow(ctx,
		`SELECT `+selectCols+` FROM audit_versions
		 WHERE entity_type = $1 AND entity_id = $2 AND version = $3`,
		entityType, entityID, version,
	)
	return scanRecord(row)
}

// GetLatest retrieves the most recent version of an entity.
func (r *Reader) GetLatest(ctx context.Context, entityType, entityID string) (domain.VersionRecord, error) {
	row := r.db.QueryRow(ctx,
		`SELECT `+selectCols+` FROM audit_versions
		 WHERE entity_type = $1 AND entity_id = $2
		 ORDER BY version DESC LIMIT 1`,
		entityType, entityID,
	)
	return scanRecord(row)
}

// GetAtTime retrieves the version of an entity that was current at time t.
func (r *Reader) GetAtTime(ctx context.Context, entityType, entityID string, t time.Time) (domain.VersionRecord, error) {
	row := r.db.QueryRow(ctx,
		`SELECT `+selectCols+` FROM audit_versions
		 WHERE entity_type = $1 AND entity_id = $2 AND created_at <= $3
		 ORDER BY created_at DESC, version DESC LIMIT 1`,
		entityType, entityID, t,
	)
	return scanRecord(row)
}

// ListVersions returns a paginated list of version records for an entity.
func (r *Reader) ListVersions(ctx context.Context, entityType, entityID string, opts ...port.ListOption) ([]domain.VersionRecord, error) {
	o := port.ApplyListOptions(opts...)

	order := "DESC"
	if o.OrderAsc {
		order = "ASC"
	}

	qdb, ok := r.db.(queryDB)
	if !ok {
		return nil, fmt.Errorf("postgres: db does not support Query method")
	}

	rows, err := qdb.Query(ctx,
		fmt.Sprintf(
			`SELECT %s FROM audit_versions
			 WHERE entity_type = $1 AND entity_id = $2
			 ORDER BY version %s LIMIT $3 OFFSET $4`,
			selectCols, order,
		),
		entityType, entityID, o.Limit, o.Offset,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list versions: %w", err)
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
		return nil, fmt.Errorf("postgres: rows iteration: %w", err)
	}
	return records, nil
}

// scanRecord scans a single row into a VersionRecord.
func scanRecord(row pgx.Row) (domain.VersionRecord, error) {
	var rec domain.VersionRecord
	var strategyInt int16
	var metaJSON []byte

	err := row.Scan(
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
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.VersionRecord{}, domain.ErrNotFound
		}
		return domain.VersionRecord{}, fmt.Errorf("postgres: scan: %w", err)
	}

	rec.Strategy = domain.Strategy(strategyInt)

	if err := json.Unmarshal(metaJSON, &rec.Metadata); err != nil {
		return domain.VersionRecord{}, fmt.Errorf("postgres: unmarshal metadata: %w", err)
	}

	return rec, nil
}

// scanRows scans the current row from a pgx.Rows into a VersionRecord.
func scanRows(rows pgx.Rows) (domain.VersionRecord, error) {
	var rec domain.VersionRecord
	var strategyInt int16
	var metaJSON []byte

	err := rows.Scan(
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
		return domain.VersionRecord{}, fmt.Errorf("postgres: scan row: %w", err)
	}

	rec.Strategy = domain.Strategy(strategyInt)

	if err := json.Unmarshal(metaJSON, &rec.Metadata); err != nil {
		return domain.VersionRecord{}, fmt.Errorf("postgres: unmarshal metadata: %w", err)
	}

	return rec, nil
}
