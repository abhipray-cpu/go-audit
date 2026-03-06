//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/abhipray-cpu/go-audit/adapter/postgres"
	"github.com/jackc/pgx/v5"
)

// testDSN reads the Postgres connection string from the environment.
// Falls back to a sensible docker default (matches docker-compose.yml).
func testDSN() string {
	if dsn := os.Getenv("AUDIT_TEST_POSTGRES_DSN"); dsn != "" {
		return dsn
	}
	return "postgres://audit:audit@localhost:5432/audit_test?sslmode=disable"
}

func setupConn(t *testing.T) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, testDSN())
	if err != nil {
		t.Fatalf("cannot connect to postgres: %v", err)
	}
	t.Cleanup(func() {
		// Clean up tables for a fresh state.
		_, _ = conn.Exec(ctx, "DROP TABLE IF EXISTS audit_versions CASCADE")
		_, _ = conn.Exec(ctx, "DROP TABLE IF EXISTS audit_schema_version CASCADE")
		conn.Close(ctx)
	})
	// Drop tables first for a clean slate.
	_, _ = conn.Exec(ctx, "DROP TABLE IF EXISTS audit_versions CASCADE")
	_, _ = conn.Exec(ctx, "DROP TABLE IF EXISTS audit_schema_version CASCADE")
	return conn
}

func TestAutoMigrate_CreatesTable(t *testing.T) {
	conn := setupConn(t)
	ctx := context.Background()

	if err := postgres.AutoMigrate(ctx, conn); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	// Verify audit_versions table exists.
	var exists bool
	err := conn.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'audit_versions'
		)`,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if !exists {
		t.Fatal("audit_versions table was not created")
	}

	// Verify audit_schema_version table exists.
	err = conn.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'audit_schema_version'
		)`,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if !exists {
		t.Fatal("audit_schema_version table was not created")
	}

	// Verify composite index exists.
	err = conn.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE indexname = 'idx_audit_versions_entity_latest'
		)`,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if !exists {
		t.Fatal("idx_audit_versions_entity_latest index was not created")
	}

	// Verify created_at index exists.
	err = conn.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE indexname = 'idx_audit_versions_entity_created_at'
		)`,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if !exists {
		t.Fatal("idx_audit_versions_entity_created_at index was not created")
	}

	// Verify schema version was recorded.
	var ver int
	err = conn.QueryRow(ctx,
		`SELECT version FROM audit_schema_version ORDER BY version DESC LIMIT 1`,
	).Scan(&ver)
	if err != nil {
		t.Fatalf("query schema version: %v", err)
	}
	if ver != 1 {
		t.Errorf("expected schema version 1, got %d", ver)
	}
}

func TestAutoMigrate_Idempotent(t *testing.T) {
	conn := setupConn(t)
	ctx := context.Background()

	// First call.
	if err := postgres.AutoMigrate(ctx, conn); err != nil {
		t.Fatalf("first AutoMigrate failed: %v", err)
	}

	// Second call — must not error.
	if err := postgres.AutoMigrate(ctx, conn); err != nil {
		t.Fatalf("second AutoMigrate failed: %v", err)
	}

	// Exactly one schema version record.
	var count int
	err := conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_schema_version`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 schema version record, got %d", count)
	}
}
