package mysql

import (
	"context"
	"fmt"
	"strings"
)

// MySQLDB abstracts the MySQL driver operations needed by [AutoMigrate],
// [Writer], and [Reader]. Both *sql.DB and *sql.Tx satisfy this interface.
type MySQLDB interface {
	ExecContext(ctx context.Context, query string, args ...any) (MySQLResult, error)
	QueryContext(ctx context.Context, query string, args ...any) (MySQLRows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) MySQLRow
}

// MySQLResult is the result of a non-query statement.
type MySQLResult interface {
	RowsAffected() (int64, error)
}

// MySQLRows is an iterator over query results.
type MySQLRows interface {
	Next() bool
	Scan(dest ...any) error
	Close() error
	Err() error
}

// MySQLRow is a single result row.
type MySQLRow interface {
	Scan(dest ...any) error
}

// schemaVersion is the current schema revision managed by this module.
const schemaVersion = 1

// AutoMigrate idempotently creates the audit_versions table and associated
// indexes using MySQL-specific syntax (InnoDB, UTF8MB4).
//
// The function is safe to call on every application start.
func AutoMigrate(ctx context.Context, db MySQLDB) error {
	// 1. Schema version tracking table.
	if _, err := db.ExecContext(ctx, ddlSchemaVersionTable); err != nil {
		return fmt.Errorf("mysql: create schema version table: %w", err)
	}

	// 2. Check current schema version.
	row := db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM audit_schema_version`,
	)
	var current int
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("mysql: read schema version: %w", err)
	}

	if current >= schemaVersion {
		return nil // already up to date
	}

	// 3. Apply migrations.
	// CREATE TABLE IF NOT EXISTS is safe, but MySQL does not support
	// CREATE INDEX IF NOT EXISTS. We catch error 1061 (duplicate key name)
	// so that repeated calls are idempotent.
	for _, stmt := range []string{
		ddlVersionsTable,
		ddlUniqueConstraint,
		ddlIndexEntityLatest,
		ddlIndexEntityCreatedAt,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if isDuplicateIndexErr(err) {
				continue // index already exists — idempotent
			}
			return fmt.Errorf("mysql: migration v%d: %w", schemaVersion, err)
		}
	}

	// 4. Record the migration.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO audit_schema_version (version, description) VALUES (?, ?)`,
		schemaVersion, "initial schema",
	); err != nil {
		return fmt.Errorf("mysql: record schema version: %w", err)
	}

	return nil
}

// ---------- DDL statements ----------

const ddlSchemaVersionTable = `
CREATE TABLE IF NOT EXISTS audit_schema_version (
    version     INT          PRIMARY KEY,
    description VARCHAR(255) NOT NULL,
    applied_at  TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

const ddlVersionsTable = `
CREATE TABLE IF NOT EXISTS audit_versions (
    id              VARCHAR(255)    PRIMARY KEY,
    entity_type     VARCHAR(255)    NOT NULL,
    entity_id       VARCHAR(255)    NOT NULL,
    version         BIGINT          NOT NULL,
    strategy        SMALLINT        NOT NULL,
    schema_version  INT             NOT NULL DEFAULT 1,
    data            LONGBLOB        NOT NULL,
    content_type    VARCHAR(255)    NOT NULL DEFAULT 'application/json',
    previous_hash   VARCHAR(255),
    hash            VARCHAR(255),
    metadata        JSON            NOT NULL,
    created_at      TIMESTAMP(3)    NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

// ddlUniqueConstraint creates a unique index on (entity_type, entity_id, version).
// MySQL does not support CREATE INDEX IF NOT EXISTS, so we use a procedure-style
// approach: attempt creation and silently ignore error 1061 (duplicate key name).
const ddlUniqueConstraint = `
CREATE UNIQUE INDEX uq_entity_version
    ON audit_versions (entity_type, entity_id, version)`

const ddlIndexEntityLatest = `
CREATE INDEX idx_audit_versions_entity_latest
    ON audit_versions (entity_type, entity_id, version DESC)`

const ddlIndexEntityCreatedAt = `
CREATE INDEX idx_audit_versions_entity_created_at
    ON audit_versions (entity_type, entity_id, created_at)`

// isDuplicateIndexErr returns true if err is MySQL error 1061 (Duplicate key name),
// which indicates the index already exists.
func isDuplicateIndexErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Duplicate key name") || strings.Contains(msg, "1061")
}
