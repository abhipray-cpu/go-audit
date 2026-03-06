package clickhouse

import (
	"context"
	"fmt"
)

// CHDB abstracts the ClickHouse driver operations needed by [AutoMigrate]
// and [Store]. Both a *sql.DB connection and pooled connection satisfy this.
type CHDB interface {
	ExecContext(ctx context.Context, query string, args ...any) (CHResult, error)
	QueryContext(ctx context.Context, query string, args ...any) (CHRows, error)
}

// CHResult is the result of a non-query statement.
type CHResult interface {
	RowsAffected() (int64, error)
}

// CHRows is an iterator over query results.
type CHRows interface {
	Next() bool
	Scan(dest ...any) error
	Close() error
	Err() error
}

// schemaVersion is the current schema revision managed by this module.
const schemaVersion = 1

// AutoMigrate idempotently creates the audit tables optimised for
// analytical queries. ClickHouse uses MergeTree engine family.
//
// Tables created:
//   - audit_versions — main fact table (ORDER BY entity_type, entity_id, version)
//   - audit_schema_version — migration tracking
//
// The function is safe to call on every application start.
func AutoMigrate(ctx context.Context, db CHDB) error {
	// 1. Schema version tracking table.
	if _, err := db.ExecContext(ctx, ddlSchemaVersionTable); err != nil {
		return fmt.Errorf("clickhouse: create schema version table: %w", err)
	}

	// 2. Check current schema version.
	rows, err := db.QueryContext(ctx,
		`SELECT COALESCE(max(version), 0) FROM audit_schema_version`,
	)
	if err != nil {
		return fmt.Errorf("clickhouse: read schema version: %w", err)
	}
	defer rows.Close()

	var current int
	if rows.Next() {
		if err := rows.Scan(&current); err != nil {
			return fmt.Errorf("clickhouse: scan schema version: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("clickhouse: schema version rows: %w", err)
	}

	if current >= schemaVersion {
		return nil // already up to date
	}

	// 3. Apply DDL.
	for _, stmt := range []string{
		ddlVersionsTable,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("clickhouse: migration v%d: %w", schemaVersion, err)
		}
	}

	// 4. Record the migration.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO audit_schema_version (version, description) VALUES (?, ?)`,
		schemaVersion, "initial schema",
	); err != nil {
		return fmt.Errorf("clickhouse: record schema version: %w", err)
	}

	return nil
}

// ---------- DDL statements ----------

const ddlSchemaVersionTable = `
CREATE TABLE IF NOT EXISTS audit_schema_version (
    version     Int32,
    description String,
    applied_at  DateTime DEFAULT now()
) ENGINE = ReplacingMergeTree(applied_at)
ORDER BY version
SETTINGS index_granularity = 512`

const ddlVersionsTable = `
CREATE TABLE IF NOT EXISTS audit_versions (
    id              String,
    entity_type     String,
    entity_id       String,
    version         Int64,
    strategy        Int16,
    schema_version  Int32 DEFAULT 1,
    data            String,
    content_type    String DEFAULT 'application/json',
    previous_hash   String,
    hash            String,
    metadata        String,
    created_at      DateTime64(3) DEFAULT now64(3)
) ENGINE = ReplacingMergeTree(created_at)
ORDER BY (entity_type, entity_id, version)
SETTINGS index_granularity = 512`
