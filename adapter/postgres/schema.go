package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DB abstracts the pgx operations needed by AutoMigrate.
// Both *pgx.Conn and *pgxpool.Pool satisfy this interface.
type DB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// schemaVersion is the current schema revision managed by this module.
const schemaVersion = 1

// AutoMigrate idempotently creates the audit_versions table and associated
// indexes. It also maintains an audit_schema_version tracking table to
// support future migrations.
//
// The function is safe to call on every application start.
func AutoMigrate(ctx context.Context, db DB) error {
	// 1. Schema version tracking table.
	if _, err := db.Exec(ctx, ddlSchemaVersionTable); err != nil {
		return fmt.Errorf("postgres: create schema version table: %w", err)
	}

	// 2. Check current schema version.
	var current int
	err := db.QueryRow(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM audit_schema_version`,
	).Scan(&current)
	if err != nil {
		return fmt.Errorf("postgres: read schema version: %w", err)
	}

	if current >= schemaVersion {
		return nil // already up to date
	}

	// 3. Apply migrations.
	for _, stmt := range []string{
		ddlVersionsTable,
		ddlUniqueConstraint,
		ddlIndexEntityLatest,
		ddlIndexEntityCreatedAt,
		ddlIndexActor,
	} {
		if _, err := db.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("postgres: migration v%d: %w", schemaVersion, err)
		}
	}

	// 4. Record the migration.
	if _, err := db.Exec(ctx,
		`INSERT INTO audit_schema_version (version, description) VALUES ($1, $2)`,
		schemaVersion, "initial schema",
	); err != nil {
		return fmt.Errorf("postgres: record schema version: %w", err)
	}

	return nil
}

// ---------- DDL statements ----------

const ddlSchemaVersionTable = `
CREATE TABLE IF NOT EXISTS audit_schema_version (
    version     INTEGER     PRIMARY KEY,
    description TEXT        NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

const ddlVersionsTable = `
CREATE TABLE IF NOT EXISTS audit_versions (
    id              TEXT        PRIMARY KEY,
    entity_type     TEXT        NOT NULL,
    entity_id       TEXT        NOT NULL,
    version         BIGINT      NOT NULL,
    strategy        SMALLINT    NOT NULL,
    schema_version  INTEGER     NOT NULL DEFAULT 1,
    data            BYTEA       NOT NULL,
    content_type    TEXT        NOT NULL DEFAULT 'application/json',
    previous_hash   TEXT,
    hash            TEXT,
    metadata        JSONB       NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

const ddlUniqueConstraint = `
DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'uq_entity_version'
    ) THEN
        ALTER TABLE audit_versions
            ADD CONSTRAINT uq_entity_version UNIQUE (entity_type, entity_id, version);
    END IF;
END $$`

const ddlIndexEntityLatest = `
CREATE INDEX IF NOT EXISTS idx_audit_versions_entity_latest
    ON audit_versions (entity_type, entity_id, version DESC)`

const ddlIndexEntityCreatedAt = `
CREATE INDEX IF NOT EXISTS idx_audit_versions_entity_created_at
    ON audit_versions (entity_type, entity_id, created_at)`

const ddlIndexActor = `
CREATE INDEX IF NOT EXISTS idx_audit_versions_actor
    ON audit_versions ((metadata->>'actor_id'), created_at DESC)`
