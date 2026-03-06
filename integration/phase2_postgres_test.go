//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/adapter/postgres"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------------------------------------------------------------------------
// Phase 2 — PG-001 to PG-012: PostgreSQL full lifecycle
// ---------------------------------------------------------------------------

// PG-001: TestPG_AutoMigrate_CreatesSchema verifies schema tables are created.
func TestPG_AutoMigrate_CreatesSchema(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	ctx := context.Background()
	// Verify audit_versions exists.
	var tableName string
	err := conn.QueryRow(ctx,
		`SELECT table_name FROM information_schema.tables WHERE table_name = 'audit_versions'`,
	).Scan(&tableName)
	if err != nil {
		t.Fatalf("audit_versions table not found: %v", err)
	}
	if tableName != "audit_versions" {
		t.Errorf("expected audit_versions, got %s", tableName)
	}

	// Verify audit_schema_version exists.
	err = conn.QueryRow(ctx,
		`SELECT table_name FROM information_schema.tables WHERE table_name = 'audit_schema_version'`,
	).Scan(&tableName)
	if err != nil {
		t.Fatalf("audit_schema_version table not found: %v", err)
	}
}

// PG-002: TestPG_AutoMigrate_Idempotent verifies AutoMigrate can be called repeatedly.
func TestPG_AutoMigrate_Idempotent(t *testing.T) {
	conn := connectPostgres(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := postgres.AutoMigrate(ctx, conn); err != nil {
			t.Fatalf("AutoMigrate call %d failed: %v", i+1, err)
		}
	}
}

// PG-003: TestPG_Write_FirstVersion verifies first version is created with version=1.
func TestPG_Write_FirstVersion(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{
		ID:    uniqueID("user"),
		Name:  "Alice",
		Email: "alice@test.com",
		Age:   25,
	}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	rec := versionSync(t, context.Background(), a, user)

	if rec.Version != 1 {
		t.Errorf("expected version 1, got %d", rec.Version)
	}
	if rec.Strategy != domain.StrategyFull {
		t.Errorf("expected StrategyFull, got %s", rec.Strategy)
	}
	if rec.EntityType != "userentity" {
		t.Errorf("expected entity type 'userentity', got %s", rec.EntityType)
	}
}

// PG-004: TestPG_Write_SecondVersion_Delta verifies v2 creation with delta strategy.
func TestPG_Write_SecondVersion_Delta(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Strategy = domain.StrategyDelta
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "Alice", Email: "a@test.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// v1
	versionSync(t, context.Background(), a, user)

	// Mutate and create v2
	user.Name = "Bob"
	rec := versionSync(t, context.Background(), a, user)

	if rec.Version != 2 {
		t.Errorf("expected version 2, got %d", rec.Version)
	}
}

// PG-005: TestPG_Write_TenVersions verifies 10 sequential versions.
func TestPG_Write_TenVersions(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "v0"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	for i := 1; i <= 10; i++ {
		user.Name = strings.Repeat("x", i)
		rec := versionSync(t, ctx, a, user)
		if rec.Version != int64(i) {
			t.Errorf("expected version %d, got %d", i, rec.Version)
		}
	}

	// Verify all 10 retrievable.
	records, err := a.ListVersions(ctx, "userentity", user.ID, port.WithLimit(20), port.WithOrderAsc())
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(records) != 10 {
		t.Fatalf("expected 10 records, got %d", len(records))
	}
}

// PG-006: TestPG_Write_DuplicateVersionRejected verifies duplicate version is rejected.
func TestPG_Write_DuplicateVersionRejected(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	w := postgres.NewWriter(conn)
	rec := domain.VersionRecord{
		ID:         uniqueID("rec"),
		EntityType: "duptest",
		EntityID:   "e1",
		Version:    1,
		Strategy:   domain.StrategyFull,
		Data:       []byte(`{"id":"e1"}`),
	}

	ctx := context.Background()
	if err := w.Save(ctx, rec); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Same entity_type + entity_id + version with different ID.
	rec.ID = uniqueID("rec2")
	err := w.Save(ctx, rec)
	if err == nil {
		t.Fatal("expected duplicate version error, got nil")
	}
	if !errors.Is(err, domain.ErrDuplicateVersion) {
		t.Errorf("expected ErrDuplicateVersion, got %v", err)
	}
}

// PG-007: TestPG_Read_GetByVersion verifies specific version retrieval.
func TestPG_Read_GetByVersion(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "Alice"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}
	versionSync(t, context.Background(), a, user)
	user.Name = "Bob"
	versionSync(t, context.Background(), a, user)

	ctx := context.Background()
	rec, err := a.GetVersion(ctx, "userentity", user.ID, 1)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if rec.Version != 1 {
		t.Errorf("expected version 1, got %d", rec.Version)
	}
}

// PG-008: TestPG_Read_GetLatest verifies latest version retrieval.
func TestPG_Read_GetLatest(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}
	versionSync(t, context.Background(), a, user)

	user.Name = "v2"
	versionSync(t, context.Background(), a, user)

	user.Name = "v3"
	versionSync(t, context.Background(), a, user)

	ctx := context.Background()
	rec, err := a.GetLatest(ctx, "userentity", user.ID)
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if rec.Version != 3 {
		t.Errorf("expected version 3, got %d", rec.Version)
	}
}

// PG-009: TestPG_Read_NotFound verifies ErrNotFound for non-existent version.
func TestPG_Read_NotFound(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	r := postgres.NewReader(conn)
	ctx := context.Background()

	_, err := r.GetByVersion(ctx, "nonexistent", "noid", 999)
	if err == nil {
		t.Fatal("expected ErrNotFound, got nil")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// PG-010: TestPG_RoundTrip_DataIntegrity verifies write→read→unmarshal round-trip.
func TestPG_RoundTrip_DataIntegrity(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{
		ID:    uniqueID("user"),
		Name:  "Alice",
		Email: "alice@test.com",
		Age:   25,
		Address: Address{
			Street: "123 Main St", City: "NYC", Country: "US", ZipCode: "10001",
		},
		Roles:    []string{"admin", "user"},
		Settings: map[string]string{"theme": "dark"},
	}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	versionSync(t, context.Background(), a, user)

	ctx := context.Background()
	rec, err := a.GetLatest(ctx, "userentity", user.ID)
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}

	var restored UserEntity
	if err := json.Unmarshal(rec.Data, &restored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if restored.Name != user.Name {
		t.Errorf("Name mismatch: %s vs %s", restored.Name, user.Name)
	}
	if restored.Email != user.Email {
		t.Errorf("Email mismatch: %s vs %s", restored.Email, user.Email)
	}
	if restored.Age != user.Age {
		t.Errorf("Age mismatch: %d vs %d", restored.Age, user.Age)
	}
	if restored.Address.City != user.Address.City {
		t.Errorf("City mismatch: %s vs %s", restored.Address.City, user.Address.City)
	}
	if len(restored.Roles) != len(user.Roles) {
		t.Errorf("Roles length mismatch: %d vs %d", len(restored.Roles), len(user.Roles))
	}
}

// PG-011: TestPG_MultipleEntityTypes verifies isolation between entity types.
func TestPG_MultipleEntityTypes(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "Alice"}
	product := &ProductEntity{ID: uniqueID("prod"), SKU: "SKU-001", Name: "Widget"}
	order := &OrderEntity{ID: uniqueID("order"), UserID: user.ID, Status: "new"}

	for _, e := range []any{user, product, order} {
		if err := a.Register(e); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)
	versionSync(t, ctx, a, product)
	versionSync(t, ctx, a, order)

	// Each entity type should have exactly 1 version.
	for _, pair := range []struct {
		typ string
		id  string
	}{
		{"userentity", user.ID},
		{"productentity", product.ID},
		{"orderentity", order.ID},
	} {
		rec, err := a.GetLatest(ctx, pair.typ, pair.id)
		if err != nil {
			t.Errorf("GetLatest(%s, %s): %v", pair.typ, pair.id, err)
			continue
		}
		if rec.Version != 1 {
			t.Errorf("%s version: expected 1, got %d", pair.typ, rec.Version)
		}
	}
}

// PG-012: TestPG_LargeEntity verifies 100KB entity round-trip.
func TestPG_LargeEntity(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	// Create a user with a large Settings map to exceed 100KB.
	settings := make(map[string]string)
	for i := 0; i < 1000; i++ {
		settings[randString(10)] = randString(100)
	}

	user := &UserEntity{
		ID:       uniqueID("user"),
		Name:     "Large Entity User",
		Settings: settings,
	}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	rec := versionSync(t, context.Background(), a, user)
	if len(rec.Data) < 100_000 {
		t.Logf("warning: entity data is %d bytes (expected >100KB)", len(rec.Data))
	}

	// Read it back.
	ctx := context.Background()
	readBack, err := a.GetLatest(ctx, "userentity", user.ID)
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if len(readBack.Data) != len(rec.Data) {
		t.Errorf("data length mismatch: wrote %d, read %d", len(rec.Data), len(readBack.Data))
	}
}
