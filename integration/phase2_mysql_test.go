//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	mysqladapter "github.com/abhipray-cpu/go-audit/adapter/mysql"
	"github.com/abhipray-cpu/go-audit/domain"
)

// ---------------------------------------------------------------------------
// Phase 2 — MY-001 to MY-007: MySQL Full Lifecycle
// ---------------------------------------------------------------------------

// sqlMySQLDB wraps *sql.DB to satisfy mysql.MySQLDB interface.
type sqlMySQLDB struct {
	db *sql.DB
}

func (s *sqlMySQLDB) ExecContext(ctx context.Context, query string, args ...any) (mysqladapter.MySQLResult, error) {
	r, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return r, nil // sql.Result satisfies MySQLResult
}

func (s *sqlMySQLDB) QueryContext(ctx context.Context, query string, args ...any) (mysqladapter.MySQLRows, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil // *sql.Rows satisfies MySQLRows
}

func (s *sqlMySQLDB) QueryRowContext(ctx context.Context, query string, args ...any) mysqladapter.MySQLRow {
	return s.db.QueryRowContext(ctx, query, args...)
}

func mysqlDB(t *testing.T) mysqladapter.MySQLDB {
	db := connectMySQL(t)
	return &sqlMySQLDB{db: db}
}

func cleanupMySQLTables(t *testing.T, db mysqladapter.MySQLDB) {
	t.Helper()
	ctx := context.Background()
	db.ExecContext(ctx, "DROP TABLE IF EXISTS audit_versions")
	db.ExecContext(ctx, "DROP TABLE IF EXISTS audit_schema_version")
}

// MY-001: TestMySQL_AutoMigrate_CreatesSchema verifies table creation.
func TestMySQL_AutoMigrate_CreatesSchema(t *testing.T) {
	db := mysqlDB(t)
	cleanupMySQLTables(t, db)

	if err := mysqladapter.AutoMigrate(context.Background(), db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	// Verify table exists by querying.
	rows, err := db.QueryContext(context.Background(), "SELECT id FROM audit_versions LIMIT 1")
	if err != nil {
		t.Fatalf("table should exist: %v", err)
	}
	rows.Close()
}

// MY-002: TestMySQL_AutoMigrate_Idempotent calls AutoMigrate 3× without error.
func TestMySQL_AutoMigrate_Idempotent(t *testing.T) {
	db := mysqlDB(t)
	cleanupMySQLTables(t, db)

	for i := 0; i < 3; i++ {
		if err := mysqladapter.AutoMigrate(context.Background(), db); err != nil {
			t.Fatalf("AutoMigrate iteration %d: %v", i, err)
		}
	}
}

// MY-003: TestMySQL_Write_FirstVersion writes a single version record.
func TestMySQL_Write_FirstVersion(t *testing.T) {
	db := mysqlDB(t)
	cleanupMySQLTables(t, db)
	mysqladapter.AutoMigrate(context.Background(), db)

	w := mysqladapter.NewWriter(db)
	rec := domain.VersionRecord{
		ID:            uniqueID("rec"),
		EntityType:    "myuser",
		EntityID:      uniqueID("eid"),
		Version:       1,
		Strategy:      domain.StrategyFull,
		SchemaVersion: 1,
		Data:          []byte(`{"name":"Alice"}`),
		ContentType:   "application/json",
		CreatedAt:     time.Now().UTC(),
	}

	if err := w.Save(context.Background(), rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// MY-004: TestMySQL_Write_MultipleVersions writes 5 sequential versions.
func TestMySQL_Write_MultipleVersions(t *testing.T) {
	db := mysqlDB(t)
	cleanupMySQLTables(t, db)
	mysqladapter.AutoMigrate(context.Background(), db)

	w := mysqladapter.NewWriter(db)
	eid := uniqueID("eid")
	for i := int64(1); i <= 5; i++ {
		rec := domain.VersionRecord{
			ID:            uniqueID("rec"),
			EntityType:    "myuser",
			EntityID:      eid,
			Version:       i,
			Strategy:      domain.StrategyFull,
			SchemaVersion: 1,
			Data:          []byte(`{"name":"v` + randString(3) + `"}`),
			ContentType:   "application/json",
			CreatedAt:     time.Now().UTC(),
		}
		if err := w.Save(context.Background(), rec); err != nil {
			t.Fatalf("Save v%d: %v", i, err)
		}
	}
}

// MY-005: TestMySQL_Read_GetByVersion retrieves a specific version.
func TestMySQL_Read_GetByVersion(t *testing.T) {
	db := mysqlDB(t)
	cleanupMySQLTables(t, db)
	mysqladapter.AutoMigrate(context.Background(), db)

	w := mysqladapter.NewWriter(db)
	r := mysqladapter.NewReader(db)
	eid := uniqueID("eid")

	rec := domain.VersionRecord{
		ID:            uniqueID("rec"),
		EntityType:    "myuser",
		EntityID:      eid,
		Version:       1,
		Strategy:      domain.StrategyFull,
		SchemaVersion: 1,
		Data:          []byte(`{"name":"Alice"}`),
		ContentType:   "application/json",
		CreatedAt:     time.Now().UTC(),
	}
	w.Save(context.Background(), rec)

	got, err := r.GetByVersion(context.Background(), "myuser", eid, 1)
	if err != nil {
		t.Fatalf("GetByVersion: %v", err)
	}
	if got.Version != 1 {
		t.Fatalf("expected version 1, got %d", got.Version)
	}
}

// MY-006: TestMySQL_Read_GetLatest retrieves the latest version.
func TestMySQL_Read_GetLatest(t *testing.T) {
	db := mysqlDB(t)
	cleanupMySQLTables(t, db)
	mysqladapter.AutoMigrate(context.Background(), db)

	w := mysqladapter.NewWriter(db)
	r := mysqladapter.NewReader(db)
	eid := uniqueID("eid")

	for i := int64(1); i <= 3; i++ {
		rec := domain.VersionRecord{
			ID:            uniqueID("rec"),
			EntityType:    "myuser",
			EntityID:      eid,
			Version:       i,
			Strategy:      domain.StrategyFull,
			SchemaVersion: 1,
			Data:          []byte(`{"name":"v` + randString(3) + `"}`),
			ContentType:   "application/json",
			CreatedAt:     time.Now().UTC(),
		}
		w.Save(context.Background(), rec)
	}

	got, err := r.GetLatest(context.Background(), "myuser", eid)
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if got.Version != 3 {
		t.Fatalf("expected version 3, got %d", got.Version)
	}
}

// MY-007: TestMySQL_RoundTrip_DataIntegrity writes and reads back entity data.
func TestMySQL_RoundTrip_DataIntegrity(t *testing.T) {
	db := mysqlDB(t)
	cleanupMySQLTables(t, db)
	mysqladapter.AutoMigrate(context.Background(), db)

	w := mysqladapter.NewWriter(db)
	r := mysqladapter.NewReader(db)
	eid := uniqueID("eid")

	original := map[string]any{
		"Name":  "Integration Test User",
		"Email": "test@example.com",
		"Age":   float64(30),
	}
	data, _ := json.Marshal(original)

	rec := domain.VersionRecord{
		ID:            uniqueID("rec"),
		EntityType:    "myuser",
		EntityID:      eid,
		Version:       1,
		Strategy:      domain.StrategyFull,
		SchemaVersion: 1,
		Data:          data,
		ContentType:   "application/json",
		CreatedAt:     time.Now().UTC(),
	}
	w.Save(context.Background(), rec)

	got, err := r.GetByVersion(context.Background(), "myuser", eid, 1)
	if err != nil {
		t.Fatalf("GetByVersion: %v", err)
	}

	var restored map[string]any
	if err := json.Unmarshal(got.Data, &restored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if restored["Name"] != original["Name"] {
		t.Fatalf("Name mismatch: %v vs %v", restored["Name"], original["Name"])
	}
	if restored["Email"] != original["Email"] {
		t.Fatalf("Email mismatch: %v vs %v", restored["Email"], original["Email"])
	}
}
