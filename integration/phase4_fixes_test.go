//go:build integration

package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	audit "github.com/abhipray-cpu/go-audit"
	chadapter "github.com/abhipray-cpu/go-audit/adapter/clickhouse"
	"github.com/abhipray-cpu/go-audit/adapter/logger"
	"github.com/abhipray-cpu/go-audit/adapter/postgres"
	"github.com/abhipray-cpu/go-audit/adapter/serializer"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/diff"
	"github.com/abhipray-cpu/go-audit/domain/port"
	"github.com/abhipray-cpu/go-audit/domain/reconstruct"
)

// ===========================================================================
// GAP 7 — Compaction E2E: Delta chain → compaction snapshot via Update()
//
// Previously compaction.go called Save() which failed with ErrDuplicateVersion
// on any real database. Fixed to call Update(). These tests verify compaction
// works against Postgres.
// ===========================================================================

// CompactEntity is a simple entity used for compaction tests.
type CompactEntity struct {
	ID    string `version:"id"`
	Value int    `version:"tracked"`
}

// CMP-001: TestCompaction_DeltaChainCompacts verifies that a delta chain
// exceeding MaxDeltaChain is compacted into a snapshot via Update().
func TestCompaction_DeltaChainCompacts(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	w := postgres.NewWriter(conn)
	r := postgres.NewReader(conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Strategy = domain.StrategyDelta
	})

	entity := &CompactEntity{ID: uniqueID("cmp"), Value: 0}
	if err := a.Register(entity); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := audit.WithActor(context.Background(), "compactor-test", domain.ActorSystem)

	// Create version 1 (always full snapshot as first version).
	entity.Value = 1
	versionSync(t, ctx, a, entity)

	// Create 4 more delta versions (chain of 4 deltas).
	for i := 2; i <= 5; i++ {
		entity.Value = i
		versionSync(t, ctx, a, entity)
	}

	// Verify we have 5 versions, with v1 full and v2-v5 delta.
	records, err := r.ListVersions(ctx, "compactentity", entity.ID, port.WithOrderAsc())
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(records) != 5 {
		t.Fatalf("expected 5 records, got %d", len(records))
	}
	if records[0].Strategy != domain.StrategyFull {
		t.Errorf("v1 should be full, got %v", records[0].Strategy)
	}
	for i := 1; i < 5; i++ {
		if records[i].Strategy != domain.StrategyDelta {
			t.Errorf("v%d should be delta, got %v", i+1, records[i].Strategy)
		}
	}

	// Run compaction with MaxDeltaChain=3 — should compact v4 (chain v2,v3,v4 = 3).
	ser := serializer.NewJSON()
	differ := diff.New()

	compactor := reconstruct.NewCompactor(reconstruct.CompactorConfig{
		Reader:        r,
		Writer:        w,
		Serializer:    ser,
		Differ:        differ,
		Registry:      a.Registry(),
		MaxDeltaChain: 3,
	})

	compacted, err := compactor.Compact(ctx, "compactentity", entity.ID)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if compacted == 0 {
		t.Fatal("expected at least 1 compaction snapshot, got 0")
	}
	t.Logf("compacted %d records", compacted)

	// After compaction, the compacted record should be a full snapshot.
	afterRecords, err := r.ListVersions(ctx, "compactentity", entity.ID, port.WithOrderAsc())
	if err != nil {
		t.Fatalf("ListVersions after compaction: %v", err)
	}

	// Find the compacted record — it should now be StrategyFull.
	foundCompacted := false
	for _, rec := range afterRecords {
		if rec.Version > 1 && rec.Strategy == domain.StrategyFull {
			foundCompacted = true
			break
		}
	}
	if !foundCompacted {
		t.Error("expected at least one delta record to be converted to full snapshot after compaction")
	}

	// Verify the entity can still be reconstructed correctly at the latest version.
	latest, err := r.GetLatest(ctx, "compactentity", entity.ID)
	if err != nil {
		t.Fatalf("GetLatest after compaction: %v", err)
	}
	var result CompactEntity
	if err := ser.Unmarshal(ctx, latest.Data, &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if result.Value != 5 {
		t.Errorf("expected Value=5 after compaction, got %d", result.Value)
	}
}

// CMP-002: TestCompaction_ShortChainNoOp verifies that compaction is a no-op
// when the delta chain is shorter than MaxDeltaChain.
func TestCompaction_ShortChainNoOp(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	w := postgres.NewWriter(conn)
	r := postgres.NewReader(conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Strategy = domain.StrategyDelta
	})

	entity := &CompactEntity{ID: uniqueID("cmp"), Value: 0}
	if err := a.Register(entity); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := audit.WithActor(context.Background(), "compactor-test", domain.ActorSystem)

	// Create 3 versions — 1 full + 2 delta.
	for i := 1; i <= 3; i++ {
		entity.Value = i
		versionSync(t, ctx, a, entity)
	}

	ser := serializer.NewJSON()
	differ := diff.New()

	compactor := reconstruct.NewCompactor(reconstruct.CompactorConfig{
		Reader:        r,
		Writer:        w,
		Serializer:    ser,
		Differ:        differ,
		Registry:      a.Registry(),
		MaxDeltaChain: 50, // default — chain of 2 is way below threshold
	})

	compacted, err := compactor.Compact(ctx, "compactentity", entity.ID)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if compacted != 0 {
		t.Errorf("expected 0 compactions for short chain, got %d", compacted)
	}
}

// CMP-003: TestCompaction_ReconstructAfterCompact verifies full round-trip:
// create a long delta chain, compact, then verify the compacted version's
// data is correct and all versions are still readable.
func TestCompaction_ReconstructAfterCompact(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	w := postgres.NewWriter(conn)
	r := postgres.NewReader(conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Strategy = domain.StrategyDelta
	})

	entity := &CompactEntity{ID: uniqueID("cmp"), Value: 0}
	if err := a.Register(entity); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := audit.WithActor(context.Background(), "compactor-test", domain.ActorSystem)

	// Create 8 versions.
	for i := 1; i <= 8; i++ {
		entity.Value = i * 10
		versionSync(t, ctx, a, entity)
	}

	ser := serializer.NewJSON()
	differ := diff.New()

	compactor := reconstruct.NewCompactor(reconstruct.CompactorConfig{
		Reader:        r,
		Writer:        w,
		Serializer:    ser,
		Differ:        differ,
		Registry:      a.Registry(),
		MaxDeltaChain: 3,
	})

	compacted, err := compactor.Compact(ctx, "compactentity", entity.ID)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if compacted == 0 {
		t.Fatal("expected at least 1 compaction, got 0")
	}
	t.Logf("compacted %d records", compacted)

	// Verify every version's Data still round-trips to the correct entity.
	// (Data always contains the full serialized entity in this implementation.)
	for v := int64(1); v <= 8; v++ {
		rec, err := r.GetByVersion(ctx, "compactentity", entity.ID, v)
		if err != nil {
			t.Fatalf("GetByVersion v%d: %v", v, err)
		}

		var ce CompactEntity
		if err := ser.Unmarshal(ctx, rec.Data, &ce); err != nil {
			t.Fatalf("Unmarshal v%d: %v", v, err)
		}

		expected := int(v) * 10
		if ce.Value != expected {
			t.Errorf("v%d: expected Value=%d, got %d (strategy=%v)", v, expected, ce.Value, rec.Strategy)
		}
	}

	// Verify at least one former-delta is now marked as full.
	records, err := r.ListVersions(ctx, "compactentity", entity.ID, port.WithOrderAsc())
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}

	fullCount := 0
	for _, rec := range records {
		if rec.Strategy == domain.StrategyFull {
			fullCount++
		}
	}
	// v1 is always full + at least 1 compacted = at least 2 full
	if fullCount < 2 {
		t.Errorf("expected at least 2 full-strategy records after compaction, got %d", fullCount)
	}
	t.Logf("after compaction: %d full / %d total", fullCount, len(records))
}

// ===========================================================================
// GAP 5 — BatchBuffer E2E: Accumulation + Flush to ClickHouse
//
// BatchBuffer exists at adapter/clickhouse/batch.go — fully implemented with
// size-threshold flush, timer-based flush, and exponential backoff retry.
// These tests exercise it against a real ClickHouse instance.
// ===========================================================================

// BB-001: TestBatchBuffer_FlushOnSize verifies that the batch buffer
// flushes to ClickHouse when the batch size threshold is reached.
func TestBatchBuffer_FlushOnSize(t *testing.T) {
	chdb := clickhouseDB(t)
	cleanupCHTables(t, chdb)

	ctx := context.Background()
	if err := chadapter.AutoMigrate(ctx, chdb); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	store := chadapter.NewStore(chdb)

	buf := chadapter.NewBatchBuffer(store, chadapter.BatchConfig{
		BatchSize:     5,
		FlushInterval: 10 * time.Second, // long interval — should flush on size, not time
		MaxRetries:    2,
		BaseBackoff:   10 * time.Millisecond,
		Logger:        logger.NewSlog(nil),
	})

	// Enqueue exactly 5 records — should trigger a size-based flush.
	entityType := uniqueID("bbsize")
	for i := 1; i <= 5; i++ {
		rec := makeCHRecord(entityType, uniqueID("eid"), int64(i))
		buf.Enqueue(rec)
	}

	// Give the background goroutine a moment to flush.
	time.Sleep(500 * time.Millisecond)

	// Query ClickHouse to verify records arrived.
	results, err := store.Query(ctx, entityType, port.WithOrderAsc())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 5 {
		t.Errorf("expected 5 records after size flush, got %d", len(results))
	}

	buf.Close()
}

// BB-002: TestBatchBuffer_FlushOnTimer verifies that the batch buffer
// flushes on the timer interval even when the batch is not full.
func TestBatchBuffer_FlushOnTimer(t *testing.T) {
	chdb := clickhouseDB(t)
	cleanupCHTables(t, chdb)

	ctx := context.Background()
	if err := chadapter.AutoMigrate(ctx, chdb); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	store := chadapter.NewStore(chdb)

	buf := chadapter.NewBatchBuffer(store, chadapter.BatchConfig{
		BatchSize:     1000,                   // high threshold — won't flush on size
		FlushInterval: 200 * time.Millisecond, // short interval — should flush on timer
		MaxRetries:    2,
		BaseBackoff:   10 * time.Millisecond,
		Logger:        logger.NewSlog(nil),
	})

	// Enqueue 3 records — well below the batch size.
	entityType := uniqueID("bbtimer")
	for i := 1; i <= 3; i++ {
		rec := makeCHRecord(entityType, uniqueID("eid"), int64(i))
		buf.Enqueue(rec)
	}

	// Wait for the timer to fire.
	time.Sleep(500 * time.Millisecond)

	results, err := store.Query(ctx, entityType, port.WithOrderAsc())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 records after timer flush, got %d", len(results))
	}

	buf.Close()
}

// BB-003: TestBatchBuffer_CloseFlushesRemaining verifies that Close()
// drains the remaining buffer and flushes it before returning.
func TestBatchBuffer_CloseFlushesRemaining(t *testing.T) {
	chdb := clickhouseDB(t)
	cleanupCHTables(t, chdb)

	ctx := context.Background()
	if err := chadapter.AutoMigrate(ctx, chdb); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	store := chadapter.NewStore(chdb)

	buf := chadapter.NewBatchBuffer(store, chadapter.BatchConfig{
		BatchSize:     1000,            // won't trigger on size
		FlushInterval: 1 * time.Minute, // won't trigger on timer
		MaxRetries:    2,
		BaseBackoff:   10 * time.Millisecond,
		Logger:        logger.NewSlog(nil),
	})

	entityType := uniqueID("bbclose")
	for i := 1; i <= 4; i++ {
		rec := makeCHRecord(entityType, uniqueID("eid"), int64(i))
		buf.Enqueue(rec)
	}

	// Close should drain + flush.
	buf.Close()

	results, err := store.Query(ctx, entityType, port.WithOrderAsc())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 4 {
		t.Errorf("expected 4 records after Close flush, got %d", len(results))
	}
}

// BB-004: TestBatchBuffer_MultipleBatches verifies that accumulating
// records across multiple size-threshold flushes works correctly.
func TestBatchBuffer_MultipleBatches(t *testing.T) {
	chdb := clickhouseDB(t)
	cleanupCHTables(t, chdb)

	ctx := context.Background()
	if err := chadapter.AutoMigrate(ctx, chdb); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	store := chadapter.NewStore(chdb)

	buf := chadapter.NewBatchBuffer(store, chadapter.BatchConfig{
		BatchSize:     3,
		FlushInterval: 10 * time.Second,
		MaxRetries:    2,
		BaseBackoff:   10 * time.Millisecond,
		Logger:        logger.NewSlog(nil),
	})

	// Enqueue 9 records — should trigger 3 flushes of 3 each.
	entityType := uniqueID("bbmulti")
	for i := 1; i <= 9; i++ {
		rec := makeCHRecord(entityType, uniqueID("eid"), int64(i))
		buf.Enqueue(rec)
	}

	// Wait for flushes.
	time.Sleep(500 * time.Millisecond)

	results, err := store.Query(ctx, entityType, port.WithOrderAsc())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 9 {
		t.Errorf("expected 9 records after multiple batch flushes, got %d", len(results))
	}

	buf.Close()
}

// ===========================================================================
// GAP 9 — MaxMetadataSizeBytes Enforcement
//
// Error types (MetadataTooLargeError, ErrMetadataTooLarge) already existed in
// domain/errors.go. Config field + enforcement were missing. Now wired in
// config.go + audit.go. These tests verify the guardrail.
// ===========================================================================

// MDS-001: TestMetadataSize_Version_RejectsOversized verifies that
// Version() rejects metadata exceeding MaxMetadataSizeBytes.
func TestMetadataSize_Version_RejectsOversized(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.MaxMetadataSizeBytes = 256 // very small limit for testing
	})

	entity := &FlatEntity{ID: uniqueID("mds"), Name: "test", Age: 1}
	if err := a.Register(entity); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Build context with large custom metadata that will exceed 256 bytes.
	ctx := context.Background()
	ctx = audit.WithActor(ctx, "user-123", domain.ActorHuman)
	ctx = audit.WithReason(ctx, "test reason")
	ctx = audit.WithCustomMetadata(ctx, "key1", strings.Repeat("x", 100))
	ctx = audit.WithCustomMetadata(ctx, "key2", strings.Repeat("y", 100))
	ctx = audit.WithCustomMetadata(ctx, "key3", strings.Repeat("z", 100))

	_, err := a.Version(ctx, entity, port.WithSync())
	if err == nil {
		t.Fatal("expected error for oversized metadata, got nil")
	}

	if !errors.Is(err, domain.ErrMetadataTooLarge) {
		t.Errorf("expected ErrMetadataTooLarge, got: %v", err)
	}

	var mtlErr *domain.MetadataTooLargeError
	if !errors.As(err, &mtlErr) {
		t.Fatalf("expected MetadataTooLargeError, got: %T", err)
	}
	if mtlErr.Limit != 256 {
		t.Errorf("expected limit=256, got %d", mtlErr.Limit)
	}
	if mtlErr.Size <= 256 {
		t.Errorf("expected size > 256, got %d", mtlErr.Size)
	}
	t.Logf("correctly rejected metadata: size=%d, limit=%d", mtlErr.Size, mtlErr.Limit)
}

// MDS-002: TestMetadataSize_Version_AllowsWithinLimit verifies that
// Version() succeeds when metadata is within the limit.
func TestMetadataSize_Version_AllowsWithinLimit(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.MaxMetadataSizeBytes = 4096 // generous limit
	})

	entity := &FlatEntity{ID: uniqueID("mds"), Name: "test", Age: 1}
	if err := a.Register(entity); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := audit.WithActor(context.Background(), "user-123", domain.ActorHuman)

	pv, err := a.Version(ctx, entity, port.WithSync())
	if err != nil {
		t.Fatalf("Version should succeed with small metadata: %v", err)
	}
	rec, err := pv.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if rec.Version != 1 {
		t.Errorf("expected version 1, got %d", rec.Version)
	}
}

// MDS-003: TestMetadataSize_Disabled verifies that setting
// MaxMetadataSizeBytes to 0 disables the check (default behavior).
func TestMetadataSize_Disabled(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	// Default — MaxMetadataSizeBytes is 0 (disabled).
	a := newAuditorPG(t, conn)

	entity := &FlatEntity{ID: uniqueID("mds"), Name: "test", Age: 1}
	if err := a.Register(entity); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Even very large metadata should pass when disabled.
	ctx := context.Background()
	ctx = audit.WithActor(ctx, "user-123", domain.ActorHuman)
	ctx = audit.WithCustomMetadata(ctx, "big", strings.Repeat("x", 10000))

	pv, err := a.Version(ctx, entity, port.WithSync())
	if err != nil {
		t.Fatalf("Version should succeed with disabled size check: %v", err)
	}
	rec, err := pv.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if rec.Version != 1 {
		t.Errorf("expected version 1, got %d", rec.Version)
	}
}

// MDS-004: TestMetadataSize_VersionInTx_RejectsOversized verifies that
// VersionInTx() also enforces the metadata size limit.
func TestMetadataSize_VersionInTx_RejectsOversized(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.MaxMetadataSizeBytes = 128 // very small
	})

	entity := &FlatEntity{ID: uniqueID("mds"), Name: "test", Age: 1}
	if err := a.Register(entity); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	ctx = audit.WithActor(ctx, "user-123", domain.ActorHuman)
	ctx = audit.WithReason(ctx, strings.Repeat("long reason ", 20))

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback(ctx)

	_, err = a.VersionInTx(ctx, tx, entity)
	if err == nil {
		t.Fatal("expected error for oversized metadata in VersionInTx, got nil")
	}

	if !errors.Is(err, domain.ErrMetadataTooLarge) {
		t.Errorf("expected ErrMetadataTooLarge, got: %v", err)
	}
}

// MDS-005: TestMetadataSize_ErrorUnwrap verifies that MetadataTooLargeError
// unwraps to both ErrMetadataTooLarge and ErrValidation.
func TestMetadataSize_ErrorUnwrap(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.MaxMetadataSizeBytes = 64 // tiny
	})

	entity := &FlatEntity{ID: uniqueID("mds"), Name: "test", Age: 1}
	if err := a.Register(entity); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := audit.WithActor(context.Background(), "user-123", domain.ActorHuman)
	ctx = audit.WithReason(ctx, strings.Repeat("reason ", 30))

	_, err := a.Version(ctx, entity, port.WithSync())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Should unwrap to both sentinels.
	if !errors.Is(err, domain.ErrMetadataTooLarge) {
		t.Error("expected errors.Is(err, ErrMetadataTooLarge) = true")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Error("expected errors.Is(err, ErrValidation) = true")
	}
}
