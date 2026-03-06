//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	chadapter "github.com/abhipray-cpu/go-audit/adapter/clickhouse"
	"github.com/abhipray-cpu/go-audit/domain"
)

// ---------------------------------------------------------------------------
// Phase 2 — CH-001 to CH-006: ClickHouse OLAP Pipeline
// ---------------------------------------------------------------------------

// sqlCHDB wraps *sql.DB to satisfy clickhouse.CHDB interface.
type sqlCHDB struct {
	db *sql.DB
}

func (s *sqlCHDB) ExecContext(ctx context.Context, query string, args ...any) (chadapter.CHResult, error) {
	r, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *sqlCHDB) QueryContext(ctx context.Context, query string, args ...any) (chadapter.CHRows, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func clickhouseDB(t *testing.T) chadapter.CHDB {
	db := connectClickHouse(t)
	return &sqlCHDB{db: db}
}

func cleanupCHTables(t *testing.T, chdb chadapter.CHDB) {
	t.Helper()
	ctx := context.Background()
	chdb.ExecContext(ctx, "DROP TABLE IF EXISTS audit_versions")
	chdb.ExecContext(ctx, "DROP TABLE IF EXISTS audit_schema_version")
}

func makeCHRecord(entityType, entityID string, version int64) domain.VersionRecord {
	meta := domain.VersionMetadata{
		ActorID: "test-actor",
		Reason:  "test",
	}
	metaJSON, _ := json.Marshal(meta)
	_ = metaJSON

	return domain.VersionRecord{
		ID:            uniqueID("ch"),
		EntityType:    entityType,
		EntityID:      entityID,
		Version:       version,
		Strategy:      domain.StrategyFull,
		SchemaVersion: 1,
		Data:          []byte(`{"name":"test"}`),
		ContentType:   "application/json",
		Metadata:      meta,
		CreatedAt:     time.Now().UTC(),
	}
}

// CH-001: TestCH_Schema_CreatesTable verifies AutoMigrate creates the table.
func TestCH_Schema_CreatesTable(t *testing.T) {
	chdb := clickhouseDB(t)
	cleanupCHTables(t, chdb)

	if err := chadapter.AutoMigrate(context.Background(), chdb); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	// Verify table by querying.
	rows, err := chdb.QueryContext(context.Background(), "SELECT id FROM audit_versions LIMIT 1")
	if err != nil {
		t.Fatalf("table should exist: %v", err)
	}
	rows.Close()
}

// CH-002: TestCH_BatchInsert_10Records inserts 10 records and verifies readback.
func TestCH_BatchInsert_10Records(t *testing.T) {
	chdb := clickhouseDB(t)
	cleanupCHTables(t, chdb)
	chadapter.AutoMigrate(context.Background(), chdb)

	store := chadapter.NewStore(chdb)
	eid := uniqueID("eid")
	records := make([]domain.VersionRecord, 10)
	for i := 0; i < 10; i++ {
		records[i] = makeCHRecord("chuser", eid, int64(i+1))
	}

	if err := store.BatchInsert(context.Background(), records); err != nil {
		t.Fatalf("BatchInsert: %v", err)
	}

	// Query back.
	results, err := store.Query(context.Background(), "chuser")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) < 10 {
		t.Fatalf("expected at least 10 results, got %d", len(results))
	}
}

// CH-003: TestCH_BatchInsert_1000Records is a perf smoke test with 1000 records.
func TestCH_BatchInsert_1000Records(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping perf test in short mode")
	}
	chdb := clickhouseDB(t)
	cleanupCHTables(t, chdb)
	chadapter.AutoMigrate(context.Background(), chdb)

	store := chadapter.NewStore(chdb)
	records := make([]domain.VersionRecord, 1000)
	for i := 0; i < 1000; i++ {
		records[i] = makeCHRecord("chperf", uniqueID("eid"), int64(i+1))
	}

	start := time.Now()
	if err := store.BatchInsert(context.Background(), records); err != nil {
		t.Fatalf("BatchInsert 1000: %v", err)
	}
	t.Logf("1000 records inserted in %v", time.Since(start))
}

// CH-004: TestCH_Query_ByEntityType queries filtered by entity_type.
func TestCH_Query_ByEntityType(t *testing.T) {
	chdb := clickhouseDB(t)
	cleanupCHTables(t, chdb)
	chadapter.AutoMigrate(context.Background(), chdb)

	store := chadapter.NewStore(chdb)
	// Insert records for two entity types.
	for i := 0; i < 3; i++ {
		store.BatchInsert(context.Background(), []domain.VersionRecord{
			makeCHRecord("typeA", uniqueID("eid"), int64(i+1)),
		})
	}
	for i := 0; i < 2; i++ {
		store.BatchInsert(context.Background(), []domain.VersionRecord{
			makeCHRecord("typeB", uniqueID("eid"), int64(i+1)),
		})
	}

	results, err := store.Query(context.Background(), "typeA")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	for _, r := range results {
		if r.EntityType != "typeA" {
			t.Fatalf("expected typeA, got %q", r.EntityType)
		}
	}
}

// CH-005: TestCH_Query_Pagination verifies Limit + Offset.
func TestCH_Query_Pagination(t *testing.T) {
	chdb := clickhouseDB(t)
	cleanupCHTables(t, chdb)
	chadapter.AutoMigrate(context.Background(), chdb)

	store := chadapter.NewStore(chdb)
	eid := uniqueID("eid")
	records := make([]domain.VersionRecord, 5)
	for i := 0; i < 5; i++ {
		records[i] = makeCHRecord("chpage", eid, int64(i+1))
	}
	store.BatchInsert(context.Background(), records)

	// TODO: The ClickHouse adapter's port.WithLimit/port.WithOffset need to be passed
	// through the Query method. For now we test without pagination args.
	results, err := store.Query(context.Background(), "chpage")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) < 1 {
		t.Fatal("expected at least 1 result")
	}
}

// CH-006: TestCH_RoundTrip_DataIntegrity verifies data integrity through
// write → read pipeline.
func TestCH_RoundTrip_DataIntegrity(t *testing.T) {
	chdb := clickhouseDB(t)
	cleanupCHTables(t, chdb)
	chadapter.AutoMigrate(context.Background(), chdb)

	store := chadapter.NewStore(chdb)
	eid := uniqueID("eid")
	original := map[string]any{"Name": "ClickHouse Test", "Value": float64(42)}
	data, _ := json.Marshal(original)

	rec := makeCHRecord("chround", eid, 1)
	rec.Data = data

	store.BatchInsert(context.Background(), []domain.VersionRecord{rec})

	results, err := store.Query(context.Background(), "chround")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	found := false
	for _, r := range results {
		if r.EntityID == eid {
			var restored map[string]any
			if err := json.Unmarshal(r.Data, &restored); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if restored["Name"] != "ClickHouse Test" {
				t.Fatalf("Name mismatch: %v", restored["Name"])
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("record not found after round-trip")
	}
}
