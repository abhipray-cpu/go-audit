//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/adapter/postgres"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/jackc/pgx/v5"
)

func newTestRecord(entityType, entityID string, version int64) domain.VersionRecord {
	meta := domain.VersionMetadata{
		ActorID:   "u-1",
		ActorType: domain.ActorHuman,
		Reason:    "test",
		Timestamp: time.Now().UTC(),
	}
	return domain.VersionRecord{
		ID:            entityID + "-v" + time.Now().Format("150405.000"),
		EntityType:    entityType,
		EntityID:      entityID,
		Version:       version,
		Strategy:      domain.StrategyFull,
		SchemaVersion: 1,
		Data:          []byte(`{"name":"test"}`),
		ContentType:   "application/json",
		Hash:          "abc123",
		PreviousHash:  "",
		Metadata:      meta,
		CreatedAt:     time.Now().UTC(),
	}
}

func TestPostgresWriter_Save(t *testing.T) {
	conn := setupConn(t)
	ctx := context.Background()

	if err := postgres.AutoMigrate(ctx, conn); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	w := postgres.NewWriter(conn)
	rec := newTestRecord("user", "u-100", 1)

	if err := w.Save(ctx, rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// SELECT it back.
	var gotID, gotEntityType, gotEntityID string
	var gotVersion int64
	err := conn.QueryRow(ctx,
		`SELECT id, entity_type, entity_id, version FROM audit_versions WHERE id = $1`,
		rec.ID,
	).Scan(&gotID, &gotEntityType, &gotEntityID, &gotVersion)
	if err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if gotID != rec.ID || gotEntityType != "user" || gotEntityID != "u-100" || gotVersion != 1 {
		t.Errorf("mismatch: got id=%s type=%s eid=%s v=%d", gotID, gotEntityType, gotEntityID, gotVersion)
	}
}

func TestPostgresWriter_SaveInTx(t *testing.T) {
	conn := setupConn(t)
	ctx := context.Background()

	if err := postgres.AutoMigrate(ctx, conn); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	w := postgres.NewWriter(conn)
	rec := newTestRecord("order", "o-200", 1)

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	if err := w.SaveInTx(ctx, tx, rec); err != nil {
		t.Fatalf("SaveInTx: %v", err)
	}

	// Before commit: not visible from the main connection.
	var count int
	err = conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_versions WHERE id = $1`, rec.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 before commit, got %d", count)
	}

	// Commit.
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// After commit: visible.
	err = conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_versions WHERE id = $1`, rec.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count query after commit: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 after commit, got %d", count)
	}
}

func TestPostgresWriter_DuplicateVersion(t *testing.T) {
	conn := setupConn(t)
	ctx := context.Background()

	if err := postgres.AutoMigrate(ctx, conn); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	w := postgres.NewWriter(conn)
	rec := newTestRecord("product", "p-300", 1)

	if err := w.Save(ctx, rec); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	// Same entity_type + entity_id + version → duplicate.
	rec2 := rec
	rec2.ID = "different-id"
	err := w.Save(ctx, rec2)
	if err == nil {
		t.Fatal("expected error for duplicate version")
	}

	if !errors.Is(err, domain.ErrDuplicateVersion) {
		t.Errorf("expected ErrDuplicateVersion, got: %v", err)
	}
}

// Ensure pgx import is used for the Begin call above.
var _ pgx.Tx
