package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------- in-memory MySQL mock ----------

type memMySQL struct {
	mu     sync.Mutex
	tables map[string][]map[string]any
}

func newMemMySQL() *memMySQL {
	return &memMySQL{tables: make(map[string][]map[string]any)}
}

func (m *memMySQL) ExecContext(_ context.Context, query string, args ...any) (MySQLResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch {
	case myContains(query, "CREATE TABLE"), myContains(query, "CREATE UNIQUE INDEX"), myContains(query, "CREATE INDEX"):
		name := extractMyTableName(query)
		if name != "" {
			if _, ok := m.tables[name]; !ok {
				m.tables[name] = nil
			}
		}
		return &myResult{0}, nil

	case myContains(query, "INSERT INTO audit_schema_version"):
		m.tables["audit_schema_version"] = append(m.tables["audit_schema_version"], map[string]any{
			"version":     args[0],
			"description": args[1],
		})
		return &myResult{1}, nil

	case myContains(query, "INSERT INTO audit_versions"):
		// Check unique constraint: (entity_type, entity_id, version)
		et := args[1].(string)
		eid := args[2].(string)
		ver := args[3].(int64)
		for _, r := range m.tables["audit_versions"] {
			if r["entity_type"] == et && r["entity_id"] == eid && toMyInt64(r["version"]) == ver {
				return nil, fmt.Errorf("Duplicate entry '%s-%s-%d' for key 'uq_entity_version'", et, eid, ver)
			}
		}
		row := map[string]any{
			"id": args[0], "entity_type": args[1], "entity_id": args[2],
			"version": args[3], "strategy": args[4], "schema_version": args[5],
			"data": args[6], "content_type": args[7],
			"previous_hash": args[8], "hash": args[9],
			"metadata": args[10], "created_at": args[11],
		}
		m.tables["audit_versions"] = append(m.tables["audit_versions"], row)
		return &myResult{1}, nil

	default:
		return &myResult{0}, nil
	}
}

func (m *memMySQL) QueryContext(_ context.Context, query string, args ...any) (MySQLRows, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if myContains(query, "FROM audit_versions") {
		et := args[0].(string)
		eid := args[1].(string)
		limit := args[2].(int)
		offset := args[3].(int)
		asc := myContains(query, "ASC")

		var matching []map[string]any
		for _, r := range m.tables["audit_versions"] {
			if r["entity_type"] == et && r["entity_id"] == eid {
				matching = append(matching, r)
			}
		}

		sort.Slice(matching, func(i, j int) bool {
			vi := toMyInt64(matching[i]["version"])
			vj := toMyInt64(matching[j]["version"])
			if asc {
				return vi < vj
			}
			return vi > vj
		})

		if offset >= len(matching) {
			matching = nil
		} else {
			end := offset + limit
			if end > len(matching) {
				end = len(matching)
			}
			matching = matching[offset:end]
		}

		return newMemRows(matching), nil
	}

	return newMemRows(nil), nil
}

func (m *memMySQL) QueryRowContext(_ context.Context, query string, args ...any) MySQLRow {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch {
	case myContains(query, "COALESCE(MAX(version"), myContains(query, "max(version"):
		rows := m.tables["audit_schema_version"]
		maxVer := 0
		for _, r := range rows {
			if v := toMyInt(r["version"]); v > maxVer {
				maxVer = v
			}
		}
		return &myRow{vals: []any{maxVer}}

	case myContains(query, "FROM audit_versions"):
		et := args[0].(string)
		eid := args[1].(string)
		var matching []map[string]any
		for _, r := range m.tables["audit_versions"] {
			if r["entity_type"] == et && r["entity_id"] == eid {
				matching = append(matching, r)
			}
		}

		if len(args) == 3 {
			// GetByVersion: 3rd arg is version
			if ver, ok := args[2].(int64); ok {
				var filtered []map[string]any
				for _, r := range matching {
					if toMyInt64(r["version"]) == ver {
						filtered = append(filtered, r)
					}
				}
				matching = filtered
			}
			// GetAtTime: 3rd arg is time.Time
			if t, ok := args[2].(time.Time); ok {
				var filtered []map[string]any
				for _, r := range matching {
					ct := r["created_at"].(time.Time)
					if !ct.After(t) {
						filtered = append(filtered, r)
					}
				}
				// Sort by created_at DESC, version DESC.
				sort.Slice(filtered, func(i, j int) bool {
					ti := filtered[i]["created_at"].(time.Time)
					tj := filtered[j]["created_at"].(time.Time)
					if !ti.Equal(tj) {
						return ti.After(tj)
					}
					return toMyInt64(filtered[i]["version"]) > toMyInt64(filtered[j]["version"])
				})
				matching = filtered
			}
		}

		if myContains(query, "ORDER BY version DESC") {
			sort.Slice(matching, func(i, j int) bool {
				return toMyInt64(matching[i]["version"]) > toMyInt64(matching[j]["version"])
			})
		}

		if len(matching) == 0 {
			return &myRow{err: errors.New("sql: no rows in result set")}
		}

		r := matching[0]
		metaStr := fmt.Sprint(r["metadata"])
		return &myRow{vals: []any{
			r["id"], r["entity_type"], r["entity_id"],
			r["version"], r["strategy"], r["schema_version"],
			r["data"], r["content_type"],
			r["previous_hash"], r["hash"],
			[]byte(metaStr), r["created_at"],
		}}

	default:
		return &myRow{err: errors.New("sql: no rows in result set")}
	}
}

// ---------- mock types ----------

type myResult struct{ affected int64 }

func (r *myResult) RowsAffected() (int64, error) { return r.affected, nil }

type myRow struct {
	vals []any
	err  error
}

func (r *myRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		if i >= len(r.vals) {
			break
		}
		switch p := d.(type) {
		case *string:
			*p = fmt.Sprint(r.vals[i])
		case *int:
			*p = toMyInt(r.vals[i])
		case *int16:
			*p = int16(toMyInt(r.vals[i]))
		case *int32:
			*p = int32(toMyInt(r.vals[i]))
		case *int64:
			*p = toMyInt64(r.vals[i])
		case *[]byte:
			switch v := r.vals[i].(type) {
			case []byte:
				*p = v
			case string:
				*p = []byte(v)
			default:
				*p = []byte(fmt.Sprint(v))
			}
		case *time.Time:
			if t, ok := r.vals[i].(time.Time); ok {
				*p = t
			}
		default:
			return fmt.Errorf("myRow: unsupported scan type %T at col %d", d, i)
		}
	}
	return nil
}

type memRows struct {
	data []map[string]any
	idx  int
}

func newMemRows(data []map[string]any) *memRows {
	return &memRows{data: data, idx: -1}
}

func (r *memRows) Next() bool {
	r.idx++
	return r.idx < len(r.data)
}

func (r *memRows) Scan(dest ...any) error {
	if r.idx >= len(r.data) {
		return fmt.Errorf("memRows: no current row")
	}
	row := r.data[r.idx]
	cols := []string{"id", "entity_type", "entity_id", "version", "strategy",
		"schema_version", "data", "content_type", "previous_hash", "hash",
		"metadata", "created_at"}
	for i, d := range dest {
		if i >= len(cols) {
			break
		}
		val := row[cols[i]]
		switch p := d.(type) {
		case *string:
			*p = fmt.Sprint(val)
		case *int:
			*p = toMyInt(val)
		case *int16:
			*p = int16(toMyInt(val))
		case *int32:
			*p = int32(toMyInt(val))
		case *int64:
			*p = toMyInt64(val)
		case *[]byte:
			switch v := val.(type) {
			case []byte:
				*p = v
			case string:
				*p = []byte(v)
			default:
				*p = []byte(fmt.Sprint(v))
			}
		case *time.Time:
			if t, ok := val.(time.Time); ok {
				*p = t
			}
		}
	}
	return nil
}

func (r *memRows) Close() error { return nil }
func (r *memRows) Err() error   { return nil }

func myContains(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

func extractMyTableName(ddl string) string {
	lower := strings.ToLower(ddl)
	for _, prefix := range []string{
		"create table if not exists ",
		"create table ",
	} {
		idx := strings.Index(lower, prefix)
		if idx >= 0 {
			rest := strings.TrimSpace(ddl[idx+len(prefix):])
			end := strings.IndexAny(rest, " (\n")
			if end < 0 {
				return rest
			}
			return rest[:end]
		}
	}
	return ""
}

func toMyInt(v any) int {
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

func toMyInt64(v any) int64 {
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

func makeMySQLRecord(id, entityType, entityID string, version int64) domain.VersionRecord {
	meta := domain.VersionMetadata{ActorID: "test-actor", Reason: "unit test"}
	return domain.VersionRecord{
		ID:            id,
		EntityType:    entityType,
		EntityID:      entityID,
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

func TestMySQL_Schema(t *testing.T) {
	db := newMemMySQL()
	ctx := context.Background()

	if err := AutoMigrate(ctx, db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

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

func TestMySQL_SchemaIdempotent(t *testing.T) {
	db := newMemMySQL()
	ctx := context.Background()

	if err := AutoMigrate(ctx, db); err != nil {
		t.Fatalf("first AutoMigrate: %v", err)
	}
	if err := AutoMigrate(ctx, db); err != nil {
		t.Fatalf("second AutoMigrate: %v", err)
	}

	db.mu.Lock()
	n := len(db.tables["audit_schema_version"])
	db.mu.Unlock()
	if n != 1 {
		t.Errorf("expected 1 schema version entry, got %d", n)
	}
}

func TestMySQL_DDL_InnoDB(t *testing.T) {
	if !strings.Contains(ddlVersionsTable, "ENGINE=InnoDB") {
		t.Error("DDL missing ENGINE=InnoDB")
	}
}

func TestMySQL_DDL_UTF8MB4(t *testing.T) {
	if !strings.Contains(ddlVersionsTable, "utf8mb4") {
		t.Error("DDL missing utf8mb4 charset")
	}
}

// ---------- Writer Tests ----------

func TestMySQL_Writer_Save(t *testing.T) {
	db := newMemMySQL()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	w := NewWriter(db)
	rec := makeMySQLRecord("w1", "user", "u1", 1)

	if err := w.Save(ctx, rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	db.mu.Lock()
	n := len(db.tables["audit_versions"])
	db.mu.Unlock()
	if n != 1 {
		t.Errorf("expected 1 row, got %d", n)
	}
}

func TestMySQL_Writer_DuplicateVersion(t *testing.T) {
	db := newMemMySQL()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	w := NewWriter(db)
	rec := makeMySQLRecord("dup1", "user", "u1", 1)
	_ = w.Save(ctx, rec)

	rec2 := makeMySQLRecord("dup2", "user", "u1", 1) // same entity+version
	err := w.Save(ctx, rec2)
	if err == nil {
		t.Fatal("expected ErrDuplicateVersion for duplicate, got nil")
	}
	if !errors.Is(err, domain.ErrDuplicateVersion) {
		t.Errorf("expected ErrDuplicateVersion, got: %v", err)
	}
}

func TestMySQL_Writer_SaveInTx(t *testing.T) {
	db := newMemMySQL()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	w := NewWriter(db)
	// Use the same db as "transaction" for mock purposes.
	rec := makeMySQLRecord("tx1", "user", "u1", 1)
	if err := w.SaveInTx(ctx, db, rec); err != nil {
		t.Fatalf("SaveInTx: %v", err)
	}
}

func TestMySQL_Writer_SaveInTx_InvalidTx(t *testing.T) {
	db := newMemMySQL()
	ctx := context.Background()

	w := NewWriter(db)
	rec := makeMySQLRecord("tx2", "user", "u1", 1)
	err := w.SaveInTx(ctx, "not-a-tx", rec)
	if err == nil {
		t.Fatal("expected error for invalid tx type")
	}
}

// ---------- Reader Tests ----------

func TestMySQL_Reader_GetByVersion(t *testing.T) {
	db := newMemMySQL()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	w := NewWriter(db)
	_ = w.Save(ctx, makeMySQLRecord("gb1", "user", "u1", 1))
	_ = w.Save(ctx, makeMySQLRecord("gb2", "user", "u1", 2))

	r := NewReader(db)
	rec, err := r.GetByVersion(ctx, "user", "u1", 2)
	if err != nil {
		t.Fatalf("GetByVersion: %v", err)
	}
	if rec.Version != 2 {
		t.Errorf("Version = %d, want 2", rec.Version)
	}
}

func TestMySQL_Reader_GetLatest(t *testing.T) {
	db := newMemMySQL()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	w := NewWriter(db)
	_ = w.Save(ctx, makeMySQLRecord("gl1", "user", "u1", 1))
	_ = w.Save(ctx, makeMySQLRecord("gl2", "user", "u1", 2))
	_ = w.Save(ctx, makeMySQLRecord("gl3", "user", "u1", 3))

	r := NewReader(db)
	rec, err := r.GetLatest(ctx, "user", "u1")
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if rec.Version != 3 {
		t.Errorf("Version = %d, want 3", rec.Version)
	}
}

func TestMySQL_Reader_GetAtTime(t *testing.T) {
	db := newMemMySQL()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	w := NewWriter(db)
	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	r1 := makeMySQLRecord("at1", "user", "u1", 1)
	r1.CreatedAt = t1
	r2 := makeMySQLRecord("at2", "user", "u1", 2)
	r2.CreatedAt = t2

	_ = w.Save(ctx, r1)
	_ = w.Save(ctx, r2)

	reader := NewReader(db)
	// Query at a time between the two records.
	rec, err := reader.GetAtTime(ctx, "user", "u1", time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetAtTime: %v", err)
	}
	if rec.Version != 1 {
		t.Errorf("Version = %d, want 1", rec.Version)
	}
}

func TestMySQL_Reader_ListVersions(t *testing.T) {
	db := newMemMySQL()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	w := NewWriter(db)
	for i := int64(1); i <= 5; i++ {
		_ = w.Save(ctx, makeMySQLRecord(fmt.Sprintf("lv%d", i), "user", "u1", i))
	}

	r := NewReader(db)
	results, err := r.ListVersions(ctx, "user", "u1", port.WithLimit(3))
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestMySQL_Reader_NotFound(t *testing.T) {
	db := newMemMySQL()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	r := NewReader(db)
	_, err := r.GetByVersion(ctx, "user", "nonexistent", 1)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestMySQL_MetadataRoundTrip(t *testing.T) {
	db := newMemMySQL()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	w := NewWriter(db)
	rec := makeMySQLRecord("meta1", "user", "u1", 1)
	rec.Metadata.ActorID = "actor-99"
	rec.Metadata.Custom = map[string]string{"env": "staging"}
	_ = w.Save(ctx, rec)

	r := NewReader(db)
	got, err := r.GetLatest(ctx, "user", "u1")
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if got.Metadata.ActorID != "actor-99" {
		t.Errorf("ActorID = %q, want %q", got.Metadata.ActorID, "actor-99")
	}
}

// ---------- Concurrent Tests ----------

func TestMySQL_ConcurrentWrites(t *testing.T) {
	db := newMemMySQL()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	w := NewWriter(db)

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			rec := makeMySQLRecord(
				fmt.Sprintf("cw-%d", id), "user",
				fmt.Sprintf("u%d", id), 1,
			)
			if err := w.Save(ctx, rec); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent Save error: %v", err)
	}

	db.mu.Lock()
	n := len(db.tables["audit_versions"])
	db.mu.Unlock()
	if n != goroutines {
		t.Errorf("expected %d rows, got %d", goroutines, n)
	}
}

func TestMySQL_IsDuplicateKeyErr(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("Duplicate entry 'x' for key 'uq_entity_version'"), true},
		{fmt.Errorf("Error 1062: duplicate key"), true},
		{fmt.Errorf("UNIQUE constraint violated"), true},
		{errors.New("connection refused"), false},
		{nil, false},
	}

	for _, tt := range tests {
		got := isDuplicateKeyErr(tt.err)
		if got != tt.want {
			t.Errorf("isDuplicateKeyErr(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

// verify JSON metadata serialization
func TestMySQL_MetadataJSON(t *testing.T) {
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
	if out.ActorID != "a" || out.Custom["k"] != "v" {
		t.Error("metadata round-trip failed")
	}
}

func BenchmarkMySQL_Save(b *testing.B) {
	db := newMemMySQL()
	ctx := context.Background()
	_ = AutoMigrate(ctx, db)

	w := NewWriter(db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.Save(ctx, makeMySQLRecord(
			fmt.Sprintf("bench-%d", i), "user",
			fmt.Sprintf("u%d", i), 1,
		))
	}
}
