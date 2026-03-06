//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/adapter/postgres"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

func seedVersions(t *testing.T, ctx context.Context, w *postgres.Writer, entityType, entityID string, n int) []domain.VersionRecord {
	t.Helper()
	var recs []domain.VersionRecord
	for i := 1; i <= n; i++ {
		rec := newTestRecord(entityType, entityID, int64(i))
		rec.ID = entityID + "-v" + time.Now().Format("150405.000000") + "-" + string(rune('a'+i))
		rec.CreatedAt = time.Now().UTC().Add(time.Duration(i) * time.Second)
		time.Sleep(10 * time.Millisecond) // ensure distinct timestamps
		if err := w.Save(ctx, rec); err != nil {
			t.Fatalf("seed version %d: %v", i, err)
		}
		recs = append(recs, rec)
	}
	return recs
}

func TestPostgresReader_GetByVersion(t *testing.T) {
	conn := setupConn(t)
	ctx := context.Background()

	if err := postgres.AutoMigrate(ctx, conn); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	w := postgres.NewWriter(conn)
	r := postgres.NewReader(conn)

	recs := seedVersions(t, ctx, w, "user", "u-400", 3)

	got, err := r.GetByVersion(ctx, "user", "u-400", 2)
	if err != nil {
		t.Fatalf("GetByVersion: %v", err)
	}
	if got.ID != recs[1].ID {
		t.Errorf("expected ID %s, got %s", recs[1].ID, got.ID)
	}
	if got.Version != 2 {
		t.Errorf("expected version 2, got %d", got.Version)
	}
}

func TestPostgresReader_GetLatest(t *testing.T) {
	conn := setupConn(t)
	ctx := context.Background()

	if err := postgres.AutoMigrate(ctx, conn); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	w := postgres.NewWriter(conn)
	r := postgres.NewReader(conn)

	recs := seedVersions(t, ctx, w, "user", "u-500", 3)

	got, err := r.GetLatest(ctx, "user", "u-500")
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if got.ID != recs[2].ID {
		t.Errorf("expected latest ID %s, got %s", recs[2].ID, got.ID)
	}
	if got.Version != 3 {
		t.Errorf("expected version 3, got %d", got.Version)
	}
}

func TestPostgresReader_GetAtTime(t *testing.T) {
	conn := setupConn(t)
	ctx := context.Background()

	if err := postgres.AutoMigrate(ctx, conn); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	w := postgres.NewWriter(conn)
	r := postgres.NewReader(conn)

	recs := seedVersions(t, ctx, w, "user", "u-600", 3)

	// Query at a time between version 2 and 3.
	midpoint := recs[1].CreatedAt.Add(500 * time.Millisecond)

	got, err := r.GetAtTime(ctx, "user", "u-600", midpoint)
	if err != nil {
		t.Fatalf("GetAtTime: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("expected version 2 at midpoint, got %d", got.Version)
	}
}

func TestPostgresReader_ListVersions(t *testing.T) {
	conn := setupConn(t)
	ctx := context.Background()

	if err := postgres.AutoMigrate(ctx, conn); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	w := postgres.NewWriter(conn)
	r := postgres.NewReader(conn)

	seedVersions(t, ctx, w, "user", "u-700", 5)

	// Page 1: first 2, ascending.
	page1, err := r.ListVersions(ctx, "user", "u-700",
		port.WithLimit(2), port.WithOffset(0), port.WithOrderAsc())
	if err != nil {
		t.Fatalf("ListVersions page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("expected 2 records, got %d", len(page1))
	}
	if page1[0].Version != 1 || page1[1].Version != 2 {
		t.Errorf("expected versions [1,2], got [%d,%d]", page1[0].Version, page1[1].Version)
	}

	// Page 2.
	page2, err := r.ListVersions(ctx, "user", "u-700",
		port.WithLimit(2), port.WithOffset(2), port.WithOrderAsc())
	if err != nil {
		t.Fatalf("ListVersions page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("expected 2 records, got %d", len(page2))
	}
	if page2[0].Version != 3 || page2[1].Version != 4 {
		t.Errorf("expected versions [3,4], got [%d,%d]", page2[0].Version, page2[1].Version)
	}
}

func TestPostgresReader_NotFound(t *testing.T) {
	conn := setupConn(t)
	ctx := context.Background()

	if err := postgres.AutoMigrate(ctx, conn); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	r := postgres.NewReader(conn)

	_, err := r.GetByVersion(ctx, "nonexistent", "x-999", 1)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}

	_, err = r.GetLatest(ctx, "nonexistent", "x-999")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for GetLatest, got: %v", err)
	}

	_, err = r.GetAtTime(ctx, "nonexistent", "x-999", time.Now())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for GetAtTime, got: %v", err)
	}
}
