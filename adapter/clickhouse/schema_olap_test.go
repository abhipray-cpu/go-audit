package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------- in-memory ClickHouse mock ----------

type memCH struct {
	mu     sync.Mutex
	tables map[string][]map[string]any // table name → rows
}

func newMemCH() *memCH {
	return &memCH{tables: make(map[string][]map[string]any)}
}

func (m *memCH) ExecContext(_ context.Context, query string, args ...any) (CHResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch {
	case chContains(query, "CREATE TABLE"):
		name := extractTableName(query)
		if _, ok := m.tables[name]; !ok {
			m.tables[name] = nil
		}
		return &chResult{0}, nil

	case chContains(query, "INSERT INTO audit_schema_version"):
		m.tables["audit_schema_version"] = append(m.tables["audit_schema_version"], map[string]any{
			"version":     args[0],
			"description": args[1],
		})
		return &chResult{1}, nil

	case chContains(query, "INSERT INTO audit_versions"):
		row := map[string]any{
			"id":             args[0],
			"entity_type":    args[1],
			"entity_id":      args[2],
			"version":        args[3],
			"strategy":       args[4],
			"schema_version": args[5],
			"data":           args[6],
			"content_type":   args[7],
			"previous_hash":  args[8],
			"hash":           args[9],
			"metadata":       args[10],
			"created_at":     args[11],
		}
		m.tables["audit_versions"] = append(m.tables["audit_versions"], row)
		return &chResult{1}, nil

	default:
		return &chResult{0}, nil
	}
}

func (m *memCH) QueryContext(_ context.Context, query string, args ...any) (CHRows, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch {
	case chContains(query, "max(version)") && chContains(query, "audit_schema_version"):
		rows := m.tables["audit_schema_version"]
		maxVer := 0
		for _, r := range rows {
			if v, ok := r["version"].(int); ok && v > maxVer {
				maxVer = v
			}
		}
		return &chRows{data: [][]any{{maxVer}}, idx: -1}, nil

	case chContains(query, "FROM audit_versions"):
		entityType := args[0].(string)
		limit := args[1].(int)
		offset := args[2].(int)
		asc := chContains(query, "ASC")

		var matching []map[string]any
		for _, r := range m.tables["audit_versions"] {
			if r["entity_type"] == entityType {
				matching = append(matching, r)
			}
		}

		// Sort by version.
		sort.Slice(matching, func(i, j int) bool {
			vi := toInt64(matching[i]["version"])
			vj := toInt64(matching[j]["version"])
			if asc {
				return vi < vj
			}
			return vi > vj
		})

		// Apply offset + limit.
		if offset >= len(matching) {
			matching = nil
		} else {
			end := offset + limit
			if end > len(matching) {
				end = len(matching)
			}
			matching = matching[offset:end]
		}

		var data [][]any
		for _, r := range matching {
			data = append(data, []any{
				r["id"],
				r["entity_type"],
				r["entity_id"],
				r["version"],
				r["strategy"],
				r["schema_version"],
				r["data"],
				r["content_type"],
				r["previous_hash"],
				r["hash"],
				r["metadata"],
				r["created_at"],
			})
		}
		return &chRows{data: data, idx: -1}, nil

	default:
		return &chRows{idx: -1}, nil
	}
}

type chResult struct{ affected int64 }

func (r *chResult) RowsAffected() (int64, error) { return r.affected, nil }

type chRows struct {
	data [][]any
	idx  int
}

func (r *chRows) Next() bool {
	r.idx++
	return r.idx < len(r.data)
}

func (r *chRows) Scan(dest ...any) error {
	if r.idx >= len(r.data) {
		return fmt.Errorf("chRows: no current row")
	}
	row := r.data[r.idx]
	for i, d := range dest {
		if i >= len(row) {
			break
		}
		switch p := d.(type) {
		case *string:
			*p = fmt.Sprint(row[i])
		case *int:
			*p = toInt(row[i])
		case *int16:
			*p = int16(toInt(row[i]))
		case *int32:
			*p = int32(toInt(row[i]))
		case *int64:
			*p = toInt64(row[i])
		case *time.Time:
			if t, ok := row[i].(time.Time); ok {
				*p = t
			}
		default:
			return fmt.Errorf("chRows: unsupported scan type %T at col %d", d, i)
		}
	}
	return nil
}

func (r *chRows) Close() error { return nil }
func (r *chRows) Err() error   { return nil }

func chContains(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

func extractTableName(ddl string) string {
	lower := strings.ToLower(ddl)
	idx := strings.Index(lower, "create table if not exists ")
	if idx < 0 {
		idx = strings.Index(lower, "create table ")
		if idx < 0 {
			return ""
		}
		idx += len("create table ")
	} else {
		idx += len("create table if not exists ")
	}
	rest := strings.TrimSpace(ddl[idx:])
	end := strings.IndexAny(rest, " (\n")
	if end < 0 {
		return rest
	}
	return rest[:end]
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int16:
		return int(n)
	case int32:
		return int(n)
	case int64:
		return int(n)
	default:
		return 0
	}
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int16:
		return int64(n)
	case int32:
		return int64(n)
	default:
		return 0
	}
}

// ---------- helpers ----------

func makeCHRecord(id string, entityType string, version int64) domain.VersionRecord {
	meta := domain.VersionMetadata{ActorID: "test", Reason: "unit test"}
	return domain.VersionRecord{
		ID:            id,
		EntityType:    entityType,
		EntityID:      id,
		Version:       version,
		Strategy:      domain.StrategyFull,
		SchemaVersion: 1,
		Data:          []byte(`{"name":"test"}`),
		ContentType:   "application/json",
		Metadata:      meta,
		CreatedAt:     time.Now(),
	}
}

// ---------- Schema Tests ----------

func TestClickHouse_Schema(t *testing.T) {
	db := newMemCH()
	ctx := context.Background()

	if err := AutoMigrate(ctx, db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	// Both tables should exist.
	db.mu.Lock()
	_, hasVersions := db.tables["audit_versions"]
	_, hasSchema := db.tables["audit_schema_version"]
	db.mu.Unlock()

	if !hasVersions {
		t.Error("audit_versions table not created")
	}
	if !hasSchema {
		t.Error("audit_schema_version table not created")
	}
}

func TestClickHouse_SchemaIdempotent(t *testing.T) {
	db := newMemCH()
	ctx := context.Background()

	if err := AutoMigrate(ctx, db); err != nil {
		t.Fatalf("first AutoMigrate: %v", err)
	}
	if err := AutoMigrate(ctx, db); err != nil {
		t.Fatalf("second AutoMigrate: %v", err)
	}

	// Schema version should only have one entry.
	db.mu.Lock()
	n := len(db.tables["audit_schema_version"])
	db.mu.Unlock()
	if n != 1 {
		t.Errorf("expected 1 schema version entry, got %d", n)
	}
}

func TestClickHouse_DDL_OrderBy(t *testing.T) {
	if !strings.Contains(ddlVersionsTable, "ORDER BY (entity_type, entity_id, version)") {
		t.Error("DDL missing ORDER BY (entity_type, entity_id, version)")
	}
}

func TestClickHouse_DDL_IndexGranularity(t *testing.T) {
	if !strings.Contains(ddlVersionsTable, "index_granularity = 512") {
		t.Error("DDL missing index_granularity = 512")
	}
}

// ---------- OLAP Store Tests ----------

func TestClickHouse_BatchInsert(t *testing.T) {
	db := newMemCH()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	store := NewStore(db)

	records := []domain.VersionRecord{
		makeCHRecord("r1", "user", 1),
		makeCHRecord("r2", "user", 2),
		makeCHRecord("r3", "order", 1),
	}

	if err := store.BatchInsert(ctx, records); err != nil {
		t.Fatalf("BatchInsert: %v", err)
	}

	db.mu.Lock()
	n := len(db.tables["audit_versions"])
	db.mu.Unlock()
	if n != 3 {
		t.Errorf("expected 3 rows, got %d", n)
	}
}

func TestClickHouse_Query(t *testing.T) {
	db := newMemCH()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	store := NewStore(db)

	// Insert records for two entity types.
	records := []domain.VersionRecord{
		makeCHRecord("u1", "user", 1),
		makeCHRecord("u2", "user", 2),
		makeCHRecord("u3", "user", 3),
		makeCHRecord("o1", "order", 1),
	}
	_ = store.BatchInsert(ctx, records)

	// Query user type only.
	results, err := store.Query(ctx, "user")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 user records, got %d", len(results))
	}

	// Verify all returned records are for "user".
	for _, r := range results {
		if r.EntityType != "user" {
			t.Errorf("expected entity_type=user, got %s", r.EntityType)
		}
	}
}

func TestClickHouse_QueryPagination(t *testing.T) {
	db := newMemCH()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	store := NewStore(db)

	// Insert 5 records.
	for i := int64(1); i <= 5; i++ {
		_ = store.BatchInsert(ctx, []domain.VersionRecord{
			makeCHRecord(fmt.Sprintf("p%d", i), "user", i),
		})
	}

	// Page 1: limit 2, offset 0.
	page1, err := store.Query(ctx, "user", port.WithLimit(2), port.WithOffset(0))
	if err != nil {
		t.Fatalf("Query page 1: %v", err)
	}
	if len(page1) != 2 {
		t.Errorf("page 1: expected 2 records, got %d", len(page1))
	}

	// Page 2: limit 2, offset 2.
	page2, err := store.Query(ctx, "user", port.WithLimit(2), port.WithOffset(2))
	if err != nil {
		t.Fatalf("Query page 2: %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("page 2: expected 2 records, got %d", len(page2))
	}

	// Page 3: limit 2, offset 4.
	page3, err := store.Query(ctx, "user", port.WithLimit(2), port.WithOffset(4))
	if err != nil {
		t.Fatalf("Query page 3: %v", err)
	}
	if len(page3) != 1 {
		t.Errorf("page 3: expected 1 record, got %d", len(page3))
	}
}

func TestClickHouse_QueryOrderAsc(t *testing.T) {
	db := newMemCH()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	store := NewStore(db)

	for i := int64(1); i <= 3; i++ {
		_ = store.BatchInsert(ctx, []domain.VersionRecord{
			makeCHRecord(fmt.Sprintf("a%d", i), "user", i),
		})
	}

	results, err := store.Query(ctx, "user", port.WithOrderAsc())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) < 2 {
		t.Fatal("expected at least 2 results")
	}
	if results[0].Version > results[1].Version {
		t.Error("expected ascending order")
	}
}

func TestClickHouse_PointQuery(t *testing.T) {
	db := newMemCH()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	store := NewStore(db)

	_ = store.BatchInsert(ctx, []domain.VersionRecord{
		makeCHRecord("pq1", "user", 1),
	})

	results, err := store.Query(ctx, "user", port.WithLimit(1))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 record, got %d", len(results))
	}
	if results[0].ID != "pq1" {
		t.Errorf("expected ID=pq1, got %s", results[0].ID)
	}
}

func TestClickHouse_MetadataRoundTrip(t *testing.T) {
	db := newMemCH()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	store := NewStore(db)

	rec := makeCHRecord("meta1", "user", 1)
	rec.Metadata.ActorID = "user-42"
	rec.Metadata.Reason = "test metadata"
	rec.Metadata.Custom = map[string]string{"env": "test"}

	_ = store.BatchInsert(ctx, []domain.VersionRecord{rec})

	results, err := store.Query(ctx, "user")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 record, got %d", len(results))
	}
	if results[0].Metadata.ActorID != "user-42" {
		t.Errorf("metadata.ActorID = %q, want %q", results[0].Metadata.ActorID, "user-42")
	}
	if results[0].Metadata.Custom["env"] != "test" {
		t.Errorf("metadata.Custom[env] = %q, want %q", results[0].Metadata.Custom["env"], "test")
	}
}

func TestClickHouse_ConcurrentBatchInsert(t *testing.T) {
	db := newMemCH()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	store := NewStore(db)

	const goroutines = 10
	const recordsPerGoroutine = 5

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			var batch []domain.VersionRecord
			for i := 0; i < recordsPerGoroutine; i++ {
				batch = append(batch, makeCHRecord(
					fmt.Sprintf("g%d-r%d", gid, i),
					"user",
					int64(gid*recordsPerGoroutine+i+1),
				))
			}
			if err := store.BatchInsert(ctx, batch); err != nil {
				errs <- err
			}
		}(g)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent BatchInsert error: %v", err)
	}

	db.mu.Lock()
	n := len(db.tables["audit_versions"])
	db.mu.Unlock()

	expected := goroutines * recordsPerGoroutine
	if n != expected {
		t.Errorf("expected %d total records, got %d", expected, n)
	}
}

func TestClickHouse_EmptyBatchInsert(t *testing.T) {
	db := newMemCH()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	store := NewStore(db)

	if err := store.BatchInsert(ctx, nil); err != nil {
		t.Errorf("empty BatchInsert should succeed, got: %v", err)
	}
}

func TestClickHouse_QueryNoResults(t *testing.T) {
	db := newMemCH()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	store := NewStore(db)

	results, err := store.Query(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func BenchmarkClickHouse_BatchInsert(b *testing.B) {
	db := newMemCH()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	store := NewStore(db)

	records := make([]domain.VersionRecord, 100)
	for i := range records {
		records[i] = makeCHRecord(fmt.Sprintf("bench-%d", i), "user", int64(i+1))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.BatchInsert(ctx, records)
	}
}

// ensure metadata is valid JSON for round-trip
func TestClickHouse_InvalidMetadataHandled(t *testing.T) {
	db := newMemCH()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	// Manually insert a row with invalid JSON metadata.
	db.mu.Lock()
	db.tables["audit_versions"] = append(db.tables["audit_versions"], map[string]any{
		"id":             "bad-meta",
		"entity_type":    "user",
		"entity_id":      "bad-meta",
		"version":        int64(1),
		"strategy":       int16(0),
		"schema_version": int(1),
		"data":           `{"x":1}`,
		"content_type":   "application/json",
		"previous_hash":  "",
		"hash":           "",
		"metadata":       "{invalid-json",
		"created_at":     time.Now(),
	})
	db.mu.Unlock()

	store := NewStore(db)
	_, err := store.Query(ctx, "user")
	if err == nil {
		t.Error("expected error for invalid JSON metadata")
	}
}

// verify JSON marshaling used in BatchInsert
func TestClickHouse_MetadataJSON(t *testing.T) {
	meta := domain.VersionMetadata{
		ActorID: "a",
		Reason:  "r",
		Custom:  map[string]string{"k": "v"},
	}
	b, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	var out domain.VersionMetadata
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.ActorID != "a" {
		t.Errorf("ActorID = %q, want %q", out.ActorID, "a")
	}
	if out.Custom["k"] != "v" {
		t.Errorf("Custom[k] = %q, want %q", out.Custom["k"], "v")
	}
}
