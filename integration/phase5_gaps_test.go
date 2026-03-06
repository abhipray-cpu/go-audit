//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	audit "github.com/abhipray-cpu/go-audit"
	chadapter "github.com/abhipray-cpu/go-audit/adapter/clickhouse"
	s3store "github.com/abhipray-cpu/go-audit/adapter/coldstore/s3"
	"github.com/abhipray-cpu/go-audit/adapter/logger"
	mysqladapter "github.com/abhipray-cpu/go-audit/adapter/mysql"
	"github.com/abhipray-cpu/go-audit/adapter/postgres"
	"github.com/abhipray-cpu/go-audit/app"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/hooks"
	"github.com/abhipray-cpu/go-audit/domain/port"
	"github.com/jackc/pgx/v5"
)

// ===========================================================================
// GAP G1 — Retention Engine E2E against real Postgres
//
// The retention engine deletes versions older than a configured retention
// period. These tests verify the full pipeline against a real Postgres DB.
// ===========================================================================

// pgDeleter implements app.VersionDeleterPort against a real Postgres database.
type pgDeleter struct {
	conn *pgx.Conn
}

func (d *pgDeleter) DeleteBefore(ctx context.Context, entityType string, before time.Time) (int, error) {
	tag, err := d.conn.Exec(ctx,
		`DELETE FROM audit_versions WHERE entity_type = $1 AND created_at < $2`,
		entityType, before,
	)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// RET-001: TestRetention_DeletesExpiredVersions verifies that RunOnce
// deletes records older than the retention cutoff from real Postgres.
func TestRetention_DeletesExpiredVersions(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	clk := newMockClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	r := postgres.NewReader(conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Clock = clk
	})

	user := &UserEntity{ID: uniqueID("ret"), Name: "old-user"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()

	// Create 3 versions with old timestamps (Jan 2024).
	for i := 0; i < 3; i++ {
		user.Name = fmt.Sprintf("old-v%d", i+1)
		versionSync(t, ctx, a, user)
		clk.Advance(1 * time.Hour)
	}

	// Advance clock to 2026 and create 2 fresh versions.
	clk = newMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	a2 := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Clock = clk
	})
	if err := a2.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for i := 0; i < 2; i++ {
		user.Name = fmt.Sprintf("new-v%d", i+1)
		versionSync(t, ctx, a2, user)
		clk.Advance(1 * time.Hour)
	}

	// Verify 5 total versions.
	all, err := r.ListVersions(ctx, "userentity", user.ID, port.WithLimit(100), port.WithOrderAsc())
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("expected 5 versions, got %d", len(all))
	}

	// Run retention with 365-day retention (anything older than 1 year ago is deleted).
	deleter := &pgDeleter{conn: conn}
	engine := app.NewRetentionEngine(app.RetentionConfig{
		RetentionDays: map[string]int{"userentity": 365},
		Reader:        r,
		Deleter:       deleter,
		Logger:        logger.NewSlog(nil),
		Interval:      1 * time.Hour,
	})
	defer engine.Stop()

	results := engine.RunOnce(ctx)
	deleted := results["userentity"]

	if deleted != 3 {
		t.Errorf("expected 3 deleted, got %d", deleted)
	}

	// Verify only 2 remain.
	remaining, err := r.ListVersions(ctx, "userentity", user.ID, port.WithLimit(100), port.WithOrderAsc())
	if err != nil {
		t.Fatalf("ListVersions after retention: %v", err)
	}
	if len(remaining) != 2 {
		t.Errorf("expected 2 remaining, got %d", len(remaining))
	}
}

// RET-002: TestRetention_NoExpiredRecords verifies that RunOnce is a no-op
// when all records are within the retention window.
func TestRetention_NoExpiredRecords(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	r := postgres.NewReader(conn)
	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("ret"), Name: "fresh"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	deleter := &pgDeleter{conn: conn}
	engine := app.NewRetentionEngine(app.RetentionConfig{
		RetentionDays: map[string]int{"userentity": 365},
		Reader:        r,
		Deleter:       deleter,
		Logger:        logger.NewSlog(nil),
		Interval:      1 * time.Hour,
	})
	defer engine.Stop()

	results := engine.RunOnce(ctx)
	if results["userentity"] != 0 {
		t.Errorf("expected 0 deleted for fresh records, got %d", results["userentity"])
	}

	remaining, err := r.ListVersions(ctx, "userentity", user.ID, port.WithLimit(100))
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining, got %d", len(remaining))
	}
}

// RET-003: TestRetention_MultipleEntityTypes verifies that retention
// applies different cutoffs per entity type.
func TestRetention_MultipleEntityTypes(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	clk := newMockClock(time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))
	r := postgres.NewReader(conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Clock = clk
	})

	user := &UserEntity{ID: uniqueID("ret"), Name: "old-user"}
	product := &ProductEntity{ID: uniqueID("ret"), Name: "old-product", SKU: "SKU-1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register user: %v", err)
	}
	if err := a.Register(product); err != nil {
		t.Fatalf("Register product: %v", err)
	}

	ctx := context.Background()

	// Create old versions for both types.
	versionSync(t, ctx, a, user)
	versionSync(t, ctx, a, product)

	// Retention: user 30 days, product 730 days (2 years).
	// The user record (from mid-2024) is >30 days old → deleted.
	// The product record (from mid-2024) is <730 days old → kept.
	deleter := &pgDeleter{conn: conn}
	engine := app.NewRetentionEngine(app.RetentionConfig{
		RetentionDays: map[string]int{
			"userentity":    30,
			"productentity": 730,
		},
		Reader:  r,
		Deleter: deleter,
		Logger:  logger.NewSlog(nil),
	})
	defer engine.Stop()

	results := engine.RunOnce(ctx)

	if results["userentity"] != 1 {
		t.Errorf("expected 1 user deleted, got %d", results["userentity"])
	}
	if results["productentity"] != 0 {
		t.Errorf("expected 0 products deleted, got %d", results["productentity"])
	}
}

// ===========================================================================
// GAP G2 — Tiering Engine E2E: hot (PG) → warm (CH) → cold (MinIO)
//
// The tiering engine moves old records from OLTP to OLAP and from OLAP
// to cold storage based on configurable age thresholds.
//
// NOTE: The PG adapter's ListVersions(ctx, entityType, "") doesn't
// support listing *all* entities of a type (it filters by entity_id="").
// We use a pgAllReader wrapper that intercepts empty-entityID calls and
// queries the DB directly for all records of that entity_type. This
// surfaces a real integration gap between TieringEngine and PG reader.
// ===========================================================================

// pgAllReader wraps postgres.Reader but handles the empty-entityID case
// that the TieringEngine uses to discover all records of an entity type.
type pgAllReader struct {
	inner port.VersionReaderPort
	conn  *pgx.Conn
}

func (r *pgAllReader) GetByVersion(ctx context.Context, entityType, entityID string, version int64) (domain.VersionRecord, error) {
	return r.inner.GetByVersion(ctx, entityType, entityID, version)
}

func (r *pgAllReader) GetLatest(ctx context.Context, entityType, entityID string) (domain.VersionRecord, error) {
	return r.inner.GetLatest(ctx, entityType, entityID)
}

func (r *pgAllReader) GetAtTime(ctx context.Context, entityType, entityID string, at time.Time) (domain.VersionRecord, error) {
	return r.inner.GetAtTime(ctx, entityType, entityID, at)
}

func (r *pgAllReader) ListVersions(ctx context.Context, entityType, entityID string, opts ...port.ListOption) ([]domain.VersionRecord, error) {
	if entityID != "" {
		return r.inner.ListVersions(ctx, entityType, entityID, opts...)
	}
	// Empty entityID: list ALL records of this entity_type.
	o := port.ApplyListOptions(opts...)
	order := "DESC"
	if o.OrderAsc {
		order = "ASC"
	}
	rows, err := r.conn.Query(ctx,
		fmt.Sprintf(
			`SELECT id, entity_type, entity_id, version, strategy,
			        schema_version, data, content_type, previous_hash,
			        hash, metadata, created_at
			 FROM audit_versions
			 WHERE entity_type = $1
			 ORDER BY version %s LIMIT $2 OFFSET $3`, order,
		),
		entityType, o.Limit, o.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []domain.VersionRecord
	for rows.Next() {
		var rec domain.VersionRecord
		var strategyInt int16
		var metaJSON []byte
		if err := rows.Scan(
			&rec.ID, &rec.EntityType, &rec.EntityID, &rec.Version,
			&strategyInt, &rec.SchemaVersion, &rec.Data, &rec.ContentType,
			&rec.PreviousHash, &rec.Hash, &metaJSON, &rec.CreatedAt,
		); err != nil {
			return nil, err
		}
		rec.Strategy = domain.Strategy(strategyInt)
		records = append(records, rec)
	}
	return records, rows.Err()
}

// TIER-001: TestTiering_MoveToWarm verifies that records older than
// WarmAfterDays are moved from OLTP to OLAP (ClickHouse).
func TestTiering_MoveToWarm(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	chdb := clickhouseDB(t)
	cleanupCHTables(t, chdb)
	chadapter.AutoMigrate(context.Background(), chdb)

	clk := newMockClock(time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC))
	r := &pgAllReader{inner: postgres.NewReader(conn), conn: conn}
	chStore := chadapter.NewStore(chdb)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Clock = clk
	})

	user := &UserEntity{ID: uniqueID("tier"), Name: "warm-candidate"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()

	// Create 2 versions with timestamps in July 2025.
	// These are >90 days old (warm) but <730 days old (not cold) from now.
	versionSync(t, ctx, a, user)
	clk.Advance(1 * time.Hour)
	user.Name = "warm-v2"
	versionSync(t, ctx, a, user)

	engine := app.NewTieringEngine(app.TieringConfig{
		WarmAfterDays: 90,  // 90 days → records from Jul 2025 are >90d old → warm
		ColdAfterDays: 730, // 2 years → records from Jul 2025 are <730d old → not cold
		EntityTypes:   []string{"userentity"},
		Reader:        r,
		OLAP:          chStore,
		Logger:        logger.NewSlog(nil),
		Interval:      1 * time.Hour,
	})
	defer engine.Stop()

	result := engine.RunOnce(ctx)

	if result.MovedToWarm != 2 {
		t.Errorf("expected 2 moved to warm, got %d", result.MovedToWarm)
	}
	if result.MovedToCold != 0 {
		t.Errorf("expected 0 moved to cold, got %d", result.MovedToCold)
	}

	// Verify records are in ClickHouse.
	chRecords, err := chStore.Query(ctx, "userentity")
	if err != nil {
		t.Fatalf("CH Query: %v", err)
	}
	count := 0
	for _, r := range chRecords {
		if r.EntityID == user.ID {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 records in CH, got %d", count)
	}
}

// TIER-002: TestTiering_MoveToCold verifies that very old records are
// moved to cold storage (MinIO/S3).
func TestTiering_MoveToCold(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	bucket := "audit-test-tiering"
	mc := connectMinioS3(t, bucket)

	clk := newMockClock(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	pgReader := postgres.NewReader(conn)
	r := &pgAllReader{inner: pgReader, conn: conn}

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Clock = clk
	})

	user := &UserEntity{ID: uniqueID("tier"), Name: "cold-candidate"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	// Use a simple adapter to bridge minioS3Client → ColdStorePort.
	coldStore := newMinioColdStore(mc, bucket)

	engine := app.NewTieringEngine(app.TieringConfig{
		WarmAfterDays: 90,
		ColdAfterDays: 730, // Records from 2020 are >730 days old → cold
		EntityTypes:   []string{"userentity"},
		Reader:        r,
		ColdStore:     coldStore,
		Logger:        logger.NewSlog(nil),
		Interval:      1 * time.Hour,
	})
	defer engine.Stop()

	result := engine.RunOnce(ctx)

	if result.MovedToCold != 1 {
		t.Errorf("expected 1 moved to cold, got %d", result.MovedToCold)
	}

	// Verify the record is retrievable from MinIO.
	versions, err := pgReader.ListVersions(ctx, "userentity", user.ID, port.WithOrderAsc())
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) == 0 {
		t.Fatal("expected at least 1 version in OLTP still")
	}
	rec := versions[0]
	key := fmt.Sprintf("%s/%s/%d", rec.EntityType, rec.EntityID, rec.Version)

	got, err := coldStore.Get(ctx, key)
	if err != nil {
		t.Fatalf("cold store Get: %v", err)
	}
	if string(got) != string(rec.Data) {
		t.Errorf("cold store data mismatch")
	}
}

// TIER-003: TestTiering_FreshRecordsNoOp verifies that fresh records
// are not moved to any tier.
func TestTiering_FreshRecordsNoOp(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	chdb := clickhouseDB(t)
	cleanupCHTables(t, chdb)
	chadapter.AutoMigrate(context.Background(), chdb)

	r := &pgAllReader{inner: postgres.NewReader(conn), conn: conn}
	chStore := chadapter.NewStore(chdb)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("tier"), Name: "fresh-record"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	engine := app.NewTieringEngine(app.TieringConfig{
		WarmAfterDays: 90,
		ColdAfterDays: 730,
		EntityTypes:   []string{"userentity"},
		Reader:        r,
		OLAP:          chStore,
		Logger:        logger.NewSlog(nil),
		Interval:      1 * time.Hour,
	})
	defer engine.Stop()

	result := engine.RunOnce(ctx)

	if result.MovedToWarm != 0 {
		t.Errorf("expected 0 moved to warm, got %d", result.MovedToWarm)
	}
	if result.MovedToCold != 0 {
		t.Errorf("expected 0 moved to cold, got %d", result.MovedToCold)
	}
}

// minioColdStore wraps s3store.Store to implement port.ColdStorePort
// against a real MinIO instance.
type minioColdStore struct {
	store *s3store.Store
}

func newMinioColdStore(mc *minioS3Client, bucket string) *minioColdStore {
	return &minioColdStore{
		store: s3store.New(s3store.Config{
			Bucket: bucket,
			Client: mc,
		}),
	}
}

func (m *minioColdStore) Put(ctx context.Context, key string, data []byte) error {
	return m.store.Put(ctx, key, data)
}

func (m *minioColdStore) Get(ctx context.Context, key string) ([]byte, error) {
	return m.store.Get(ctx, key)
}

// ===========================================================================
// GAP G3 — Hooks wired into Auditor (BeforeWrite / BeforeRead)
//
// Previously hooks existed in Config but were never invoked by audit.go.
// BUG 8: Fixed by wiring cfg.Hooks.BeforeWrite/BeforeRead into
// versionSync, VersionInTx, GetVersion, GetLatest, GetAtTime, ListVersions.
// These tests verify the full pipeline.
// ===========================================================================

// HW-001: TestHooksWired_BeforeWrite_Blocks verifies that a rejecting
// BeforeWrite hook blocks Version() on a real Postgres Auditor.
func TestHooksWired_BeforeWrite_Blocks(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	reg := hooks.New()
	reg.OnBeforeWrite("deny-write", func(ctx context.Context, entityType, entityID string) error {
		return errors.New("write denied by hook")
	})

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Hooks = reg
	})

	user := &UserEntity{ID: uniqueID("hook"), Name: "blocked"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	_, err := a.Version(ctx, user, port.WithSync())
	if err == nil {
		t.Fatal("expected error from BeforeWrite hook, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got: %v", err)
	}
	if !strings.Contains(err.Error(), "write denied by hook") {
		t.Errorf("expected hook message in error, got: %v", err)
	}

	// Verify nothing was persisted.
	_, readErr := a.GetLatest(ctx, "userentity", user.ID)
	if readErr == nil {
		t.Error("expected no version to exist after blocked write")
	}
}

// HW-002: TestHooksWired_BeforeWrite_Allows verifies that a passing
// hook does not block writes.
func TestHooksWired_BeforeWrite_Allows(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	reg := hooks.New()
	reg.OnBeforeWrite("allow-write", func(ctx context.Context, entityType, entityID string) error {
		return nil
	})

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Hooks = reg
	})

	user := &UserEntity{ID: uniqueID("hook"), Name: "allowed"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	rec := versionSync(t, ctx, a, user)
	if rec.Version != 1 {
		t.Errorf("expected version 1, got %d", rec.Version)
	}
}

// HW-003: TestHooksWired_BeforeRead_Blocks verifies that a rejecting
// BeforeRead hook blocks GetVersion, GetLatest, GetAtTime, ListVersions.
func TestHooksWired_BeforeRead_Blocks(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	// First create a record without hooks.
	a1 := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("hook"), Name: "readable"}
	if err := a1.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx := context.Background()
	versionSync(t, ctx, a1, user)

	// Now create a new auditor with read hooks that deny.
	reg := hooks.New()
	reg.OnBeforeRead("deny-read", func(ctx context.Context, entityType, entityID string) error {
		return errors.New("read denied by hook")
	})
	a2 := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Hooks = reg
	})
	if err := a2.Register(&UserEntity{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// GetVersion should be blocked.
	_, err := a2.GetVersion(ctx, "userentity", user.ID, 1)
	if err == nil {
		t.Error("GetVersion: expected error from BeforeRead hook")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("GetVersion: expected ErrValidation, got: %v", err)
	}

	// GetLatest should be blocked.
	_, err = a2.GetLatest(ctx, "userentity", user.ID)
	if err == nil {
		t.Error("GetLatest: expected error from BeforeRead hook")
	}

	// GetAtTime should be blocked.
	_, err = a2.GetAtTime(ctx, "userentity", user.ID, time.Now())
	if err == nil {
		t.Error("GetAtTime: expected error from BeforeRead hook")
	}

	// ListVersions should be blocked.
	_, err = a2.ListVersions(ctx, "userentity", user.ID)
	if err == nil {
		t.Error("ListVersions: expected error from BeforeRead hook")
	}
}

// HW-004: TestHooksWired_VersionInTx_Blocks verifies that BeforeWrite
// hooks also block VersionInTx.
func TestHooksWired_VersionInTx_Blocks(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	reg := hooks.New()
	reg.OnBeforeWrite("deny-tx-write", func(ctx context.Context, entityType, entityID string) error {
		return errors.New("tx write denied")
	})

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Hooks = reg
	})

	user := &UserEntity{ID: uniqueID("hook"), Name: "tx-blocked"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback(ctx)

	_, err = a.VersionInTx(ctx, tx, user)
	if err == nil {
		t.Fatal("expected error from BeforeWrite hook in VersionInTx")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got: %v", err)
	}
}

// HW-005: TestHooksWired_ConditionalAccess verifies a hook that allows
// some entities and denies others.
func TestHooksWired_ConditionalAccess(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	reg := hooks.New()
	reg.OnBeforeWrite("conditional", func(ctx context.Context, entityType, entityID string) error {
		if entityType == "productentity" {
			return errors.New("products are read-only")
		}
		return nil
	})

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Hooks = reg
	})

	user := &UserEntity{ID: uniqueID("hook"), Name: "allowed-user"}
	product := &ProductEntity{ID: uniqueID("hook"), Name: "blocked-product", SKU: "SKU-1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register user: %v", err)
	}
	if err := a.Register(product); err != nil {
		t.Fatalf("Register product: %v", err)
	}

	ctx := context.Background()

	// User write should succeed.
	rec := versionSync(t, ctx, a, user)
	if rec.Version != 1 {
		t.Errorf("expected user version 1, got %d", rec.Version)
	}

	// Product write should be blocked.
	_, err := a.Version(ctx, product, port.WithSync())
	if err == nil {
		t.Fatal("expected product write to be blocked by hook")
	}
	if !strings.Contains(err.Error(), "products are read-only") {
		t.Errorf("expected 'products are read-only', got: %v", err)
	}
}

// ===========================================================================
// GAP G4 — MySQL Update/Redaction E2E
//
// Verifies that the MySQL adapter's Update() method (which was fixed to
// include the strategy column) works correctly for redaction against a
// real MySQL database.
// ===========================================================================

// MYR-001: TestMySQL_Redaction_TopLevelField verifies that RedactField
// works against a real MySQL backend.
func TestMySQL_Redaction_TopLevelField(t *testing.T) {
	mydb := mysqlDB(t)
	cleanupMySQLTables(t, mydb)

	ctx := context.Background()
	if err := mysqladapter.AutoMigrate(ctx, mydb); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	w := mysqladapter.NewWriter(mydb)
	r := mysqladapter.NewReader(mydb)

	a, err := audit.New(audit.Config{
		Writer: w,
		Reader: r,
		Logger: logger.NewSlog(nil),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(ctx)

	user := &UserEntity{ID: uniqueID("myr"), Name: "Alice", Email: "alice@example.com", Age: 30}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	versionSync(t, ctx, a, user)

	// Redact the Email field.
	redacted, err := a.RedactField(ctx, "userentity", user.ID, "Email")
	if err != nil {
		t.Fatalf("RedactField: %v", err)
	}
	if redacted != 1 {
		t.Errorf("expected 1 version redacted, got %d", redacted)
	}

	// Verify the redacted data.
	rec, err := a.GetLatest(ctx, "userentity", user.ID)
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(rec.Data, &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if result["Email"] != "[REDACTED]" {
		t.Errorf("expected Email=[REDACTED], got %v", result["Email"])
	}
	if result["Name"] != "Alice" {
		t.Errorf("expected Name=Alice, got %v", result["Name"])
	}
}

// MYR-002: TestMySQL_Update_PreservesStrategy verifies that MySQL Update
// correctly writes the strategy column.
func TestMySQL_Update_PreservesStrategy(t *testing.T) {
	mydb := mysqlDB(t)
	cleanupMySQLTables(t, mydb)

	ctx := context.Background()
	if err := mysqladapter.AutoMigrate(ctx, mydb); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	w := mysqladapter.NewWriter(mydb)
	r := mysqladapter.NewReader(mydb)

	a, err := audit.New(audit.Config{
		Writer:   w,
		Reader:   r,
		Logger:   logger.NewSlog(nil),
		Strategy: domain.StrategyDelta,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(ctx)

	user := &UserEntity{ID: uniqueID("myr"), Name: "v1", Email: "a@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Create v1 (full) and v2 (delta).
	versionSync(t, ctx, a, user)
	user.Name = "v2"
	versionSync(t, ctx, a, user)

	// Update v2's record to full strategy (simulating compaction).
	rec, err := r.GetByVersion(ctx, "userentity", user.ID, 2)
	if err != nil {
		t.Fatalf("GetByVersion: %v", err)
	}
	rec.Strategy = domain.StrategyFull
	if err := w.Update(ctx, rec); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Read back and verify strategy changed.
	updated, err := r.GetByVersion(ctx, "userentity", user.ID, 2)
	if err != nil {
		t.Fatalf("GetByVersion after update: %v", err)
	}
	if updated.Strategy != domain.StrategyFull {
		t.Errorf("expected strategy=full after update, got %v", updated.Strategy)
	}
}

// ===========================================================================
// GAP G5 — MySQL Query API (GetAtTime, ListVersions pagination)
//
// The Postgres adapter has extensive query tests; MySQL was missing these.
// ===========================================================================

// MYQ-001: TestMySQL_GetAtTime verifies point-in-time reads against MySQL.
func TestMySQL_GetAtTime(t *testing.T) {
	mydb := mysqlDB(t)
	cleanupMySQLTables(t, mydb)

	ctx := context.Background()
	if err := mysqladapter.AutoMigrate(ctx, mydb); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	w := mysqladapter.NewWriter(mydb)
	r := mysqladapter.NewReader(mydb)

	clk := newMockClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	a, err := audit.New(audit.Config{
		Writer: w,
		Reader: r,
		Logger: logger.NewSlog(nil),
		Clock:  clk,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(ctx)

	user := &UserEntity{ID: uniqueID("myq"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// v1 at Jan 1, v2 at Feb 1, v3 at Mar 1.
	versionSync(t, ctx, a, user)
	clk.Advance(31 * 24 * time.Hour) // Feb 1
	user.Name = "v2"
	versionSync(t, ctx, a, user)
	clk.Advance(28 * 24 * time.Hour) // Mar 1
	user.Name = "v3"
	versionSync(t, ctx, a, user)

	// Query at Jan 15 → should get v1.
	jan15 := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	rec, err := r.GetAtTime(ctx, "userentity", user.ID, jan15)
	if err != nil {
		t.Fatalf("GetAtTime Jan 15: %v", err)
	}
	if rec.Version != 1 {
		t.Errorf("expected v1 at Jan 15, got v%d", rec.Version)
	}

	// Query at Feb 15 → should get v2.
	feb15 := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	rec, err = r.GetAtTime(ctx, "userentity", user.ID, feb15)
	if err != nil {
		t.Fatalf("GetAtTime Feb 15: %v", err)
	}
	if rec.Version != 2 {
		t.Errorf("expected v2 at Feb 15, got v%d", rec.Version)
	}
}

// MYQ-002: TestMySQL_ListVersions_Pagination verifies limit/offset against MySQL.
func TestMySQL_ListVersions_Pagination(t *testing.T) {
	mydb := mysqlDB(t)
	cleanupMySQLTables(t, mydb)

	ctx := context.Background()
	if err := mysqladapter.AutoMigrate(ctx, mydb); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	w := mysqladapter.NewWriter(mydb)
	r := mysqladapter.NewReader(mydb)

	a, err := audit.New(audit.Config{
		Writer: w,
		Reader: r,
		Logger: logger.NewSlog(nil),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(ctx)

	user := &UserEntity{ID: uniqueID("myq"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Create 5 versions.
	for i := 1; i <= 5; i++ {
		user.Name = fmt.Sprintf("v%d", i)
		versionSync(t, ctx, a, user)
	}

	// Ascending with limit=2, offset=1.
	recs, err := r.ListVersions(ctx, "userentity", user.ID, port.WithOrderAsc(), port.WithLimit(2), port.WithOffset(1))
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records with limit=2, got %d", len(recs))
	}
	// Offset=1 from ascending → v2, v3.
	if recs[0].Version != 2 {
		t.Errorf("expected first record v2, got v%d", recs[0].Version)
	}
	if recs[1].Version != 3 {
		t.Errorf("expected second record v3, got v%d", recs[1].Version)
	}
}

// MYQ-003: TestMySQL_ListVersions_Ascending verifies ascending order.
func TestMySQL_ListVersions_Ascending(t *testing.T) {
	mydb := mysqlDB(t)
	cleanupMySQLTables(t, mydb)

	ctx := context.Background()
	if err := mysqladapter.AutoMigrate(ctx, mydb); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	w := mysqladapter.NewWriter(mydb)
	r := mysqladapter.NewReader(mydb)

	a, err := audit.New(audit.Config{
		Writer: w,
		Reader: r,
		Logger: logger.NewSlog(nil),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(ctx)

	user := &UserEntity{ID: uniqueID("myq"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for i := 1; i <= 3; i++ {
		user.Name = fmt.Sprintf("v%d", i)
		versionSync(t, ctx, a, user)
	}

	recs, err := r.ListVersions(ctx, "userentity", user.ID, port.WithOrderAsc())
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(recs) < 3 {
		t.Fatalf("expected ≥3 records, got %d", len(recs))
	}
	for i := 1; i < len(recs); i++ {
		if recs[i].Version <= recs[i-1].Version {
			t.Errorf("expected ascending order, got v%d after v%d", recs[i].Version, recs[i-1].Version)
		}
	}
}

// ===========================================================================
// GAP G6 — StorageRouter dual-write E2E
//
// The StorageRouter routes writes to OLTP (sync) + OLAP (async). Previous
// tests used a subscriber bridge; this tests the actual StorageRouter.
// ===========================================================================

// SR-001: TestStorageRouter_DualWrite_PGandCH verifies that using
// StorageRouter as the Writer causes records to appear in both Postgres
// and ClickHouse.
func TestStorageRouter_DualWrite_PGandCH(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	chdb := clickhouseDB(t)
	cleanupCHTables(t, chdb)
	chadapter.AutoMigrate(context.Background(), chdb)

	pgWriter := postgres.NewWriter(conn)
	pgReader := postgres.NewReader(conn)
	chStore := chadapter.NewStore(chdb)

	router := app.NewStorageRouter(app.RouterConfig{
		OLTP: pgWriter,
		OLAP: chStore,
	})

	a, err := audit.New(audit.Config{
		Writer: router,
		Reader: pgReader,
		Logger: logger.NewSlog(nil),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(context.Background())

	user := &UserEntity{ID: uniqueID("sr"), Name: "router-test"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	// Verify in Postgres.
	rec, err := pgReader.GetLatest(ctx, "userentity", user.ID)
	if err != nil {
		t.Fatalf("PG GetLatest: %v", err)
	}
	if rec.EntityID != user.ID {
		t.Errorf("PG entity_id = %q, want %q", rec.EntityID, user.ID)
	}

	// OLAP write is async — give it a moment.
	time.Sleep(500 * time.Millisecond)

	// Verify in ClickHouse.
	chRecords, err := chStore.Query(ctx, "userentity")
	if err != nil {
		t.Fatalf("CH Query: %v", err)
	}
	found := false
	for _, r := range chRecords {
		if r.EntityID == user.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("record not found in ClickHouse after StorageRouter dual-write")
	}
}

// SR-002: TestStorageRouter_MultipleVersions verifies that multiple
// versions all reach both backends through the StorageRouter.
func TestStorageRouter_MultipleVersions(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	chdb := clickhouseDB(t)
	cleanupCHTables(t, chdb)
	chadapter.AutoMigrate(context.Background(), chdb)

	pgWriter := postgres.NewWriter(conn)
	pgReader := postgres.NewReader(conn)
	chStore := chadapter.NewStore(chdb)

	router := app.NewStorageRouter(app.RouterConfig{
		OLTP: pgWriter,
		OLAP: chStore,
	})

	a, err := audit.New(audit.Config{
		Writer: router,
		Reader: pgReader,
		Logger: logger.NewSlog(nil),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(context.Background())

	user := &UserEntity{ID: uniqueID("sr"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	for i := 1; i <= 4; i++ {
		user.Name = fmt.Sprintf("v%d", i)
		versionSync(t, ctx, a, user)
	}

	// Verify Postgres has 4 versions.
	pgVersions, err := pgReader.ListVersions(ctx, "userentity", user.ID, port.WithLimit(100))
	if err != nil {
		t.Fatalf("PG ListVersions: %v", err)
	}
	if len(pgVersions) != 4 {
		t.Errorf("PG versions = %d, want 4", len(pgVersions))
	}

	// OLAP is async — wait briefly.
	time.Sleep(500 * time.Millisecond)

	// Verify ClickHouse has 4 records.
	chRecords, err := chStore.Query(ctx, "userentity")
	if err != nil {
		t.Fatalf("CH Query: %v", err)
	}
	count := 0
	for _, r := range chRecords {
		if r.EntityID == user.ID {
			count++
		}
	}
	if count != 4 {
		t.Errorf("CH records = %d, want 4", count)
	}
}

// SR-003: TestStorageRouter_OLAPFailureIsolated verifies that an OLAP
// failure does not affect the OLTP write.
func TestStorageRouter_OLAPFailureIsolated(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	pgWriter := postgres.NewWriter(conn)
	pgReader := postgres.NewReader(conn)

	// Use a failing OLAP that always errors.
	router := app.NewStorageRouter(app.RouterConfig{
		OLTP: pgWriter,
		OLAP: &failingOLAP{},
	})

	a, err := audit.New(audit.Config{
		Writer: router,
		Reader: pgReader,
		Logger: logger.NewSlog(nil),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(context.Background())

	user := &UserEntity{ID: uniqueID("sr"), Name: "olap-fail"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	// Version should succeed despite OLAP failure.
	rec := versionSync(t, ctx, a, user)
	if rec.Version != 1 {
		t.Errorf("expected version 1, got %d", rec.Version)
	}

	// Verify the record is in Postgres.
	got, err := pgReader.GetLatest(ctx, "userentity", user.ID)
	if err != nil {
		t.Fatalf("PG GetLatest: %v", err)
	}
	if got.EntityID != user.ID {
		t.Errorf("PG entity_id = %q, want %q", got.EntityID, user.ID)
	}
}

// failingOLAP always returns errors on BatchInsert.
type failingOLAP struct{}

func (f *failingOLAP) BatchInsert(_ context.Context, _ []domain.VersionRecord) error {
	return errors.New("OLAP unavailable")
}

func (f *failingOLAP) Query(_ context.Context, _ string, _ ...port.ListOption) ([]domain.VersionRecord, error) {
	return nil, errors.New("OLAP unavailable")
}
