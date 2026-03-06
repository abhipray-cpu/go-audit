package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time interface check.
var _ port.OLAPPort = (*Store)(nil)

// Store implements [port.OLAPPort] for ClickHouse.
//
// Store does not hold its own mutex — thread safety is delegated to the
// underlying [CHDB] implementation (e.g., *sql.DB is safe for concurrent use).
type Store struct {
	db CHDB
}

// NewStore returns a Store backed by the given ClickHouse connection.
func NewStore(db CHDB) *Store {
	return &Store{db: db}
}

const batchInsertSQL = `INSERT INTO audit_versions
    (id, entity_type, entity_id, version, strategy,
     schema_version, data, content_type,
     previous_hash, hash, metadata, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// BatchInsert writes a slice of version records to ClickHouse.
//
// Each record is inserted individually via [CHDB.ExecContext]. For
// high-throughput OLAP workloads (1 000+ records per batch), callers
// should use the ClickHouse driver's native batch protocol instead:
//
//	conn.PrepareBatch(ctx, batchInsertSQL)
//
// This sequential implementation is suitable for moderate volumes and
// ensures each insert error is reported individually.
func (s *Store) BatchInsert(ctx context.Context, records []domain.VersionRecord) error {
	for _, rec := range records {
		metaJSON, err := json.Marshal(rec.Metadata)
		if err != nil {
			return fmt.Errorf("clickhouse: marshal metadata: %w", err)
		}

		_, err = s.db.ExecContext(ctx, batchInsertSQL,
			rec.ID,
			rec.EntityType,
			rec.EntityID,
			rec.Version,
			int16(rec.Strategy),
			rec.SchemaVersion,
			string(rec.Data),
			rec.ContentType,
			rec.PreviousHash,
			rec.Hash,
			string(metaJSON),
			rec.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("clickhouse: batch insert: %w", err)
		}
	}
	return nil
}

// Pre-built query templates keyed by sort direction to avoid fmt.Sprintf
// with a dynamic ORDER BY clause, which static analysis tools flag.
var queryByOrder = map[bool]string{
	true: `SELECT id, entity_type, entity_id, version, strategy,
		        schema_version, data, content_type,
		        previous_hash, hash, metadata, created_at
		 FROM audit_versions
		 WHERE entity_type = ?
		 ORDER BY version ASC
		 LIMIT ? OFFSET ?`,
	false: `SELECT id, entity_type, entity_id, version, strategy,
		        schema_version, data, content_type,
		        previous_hash, hash, metadata, created_at
		 FROM audit_versions
		 WHERE entity_type = ?
		 ORDER BY version DESC
		 LIMIT ? OFFSET ?`,
}

// Query executes an analytical query and returns matching version records,
// filtered by entity_type with pagination support.
func (s *Store) Query(ctx context.Context, entityType string, opts ...port.ListOption) ([]domain.VersionRecord, error) {
	o := port.ApplyListOptions(opts...)

	query := queryByOrder[o.OrderAsc]

	rows, err := s.db.QueryContext(ctx, query, entityType, o.Limit, o.Offset)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: query: %w", err)
	}
	defer rows.Close()

	var records []domain.VersionRecord
	for rows.Next() {
		rec, err := scanCHRow(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("clickhouse: rows iteration: %w", err)
	}
	return records, nil
}

// scanCHRow scans the current row from CHRows into a VersionRecord.
func scanCHRow(rows CHRows) (domain.VersionRecord, error) {
	var rec domain.VersionRecord
	var strategyInt int16
	var dataStr, metaStr string

	err := rows.Scan(
		&rec.ID,
		&rec.EntityType,
		&rec.EntityID,
		&rec.Version,
		&strategyInt,
		&rec.SchemaVersion,
		&dataStr,
		&rec.ContentType,
		&rec.PreviousHash,
		&rec.Hash,
		&metaStr,
		&rec.CreatedAt,
	)
	if err != nil {
		return domain.VersionRecord{}, fmt.Errorf("clickhouse: scan row: %w", err)
	}

	rec.Strategy = domain.Strategy(strategyInt)
	rec.Data = []byte(dataStr)

	if err := json.Unmarshal([]byte(metaStr), &rec.Metadata); err != nil {
		return domain.VersionRecord{}, fmt.Errorf("clickhouse: unmarshal metadata: %w", err)
	}

	return rec, nil
}
