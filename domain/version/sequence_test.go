package version

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------- stub reader ----------

type fakeReader struct {
	latest    *domain.VersionRecord
	latestErr error
}

func (f *fakeReader) GetByVersion(_ context.Context, _, _ string, _ int64) (domain.VersionRecord, error) {
	return domain.VersionRecord{}, nil
}
func (f *fakeReader) GetLatest(_ context.Context, _, _ string) (domain.VersionRecord, error) {
	if f.latestErr != nil {
		return domain.VersionRecord{}, f.latestErr
	}
	if f.latest == nil {
		return domain.VersionRecord{}, domain.ErrNotFound
	}
	return *f.latest, nil
}
func (f *fakeReader) GetAtTime(_ context.Context, _, _ string, _ time.Time) (domain.VersionRecord, error) {
	return domain.VersionRecord{}, nil
}
func (f *fakeReader) ListVersions(_ context.Context, _, _ string, _ ...port.ListOption) ([]domain.VersionRecord, error) {
	return nil, nil
}

// ---------- tests ----------

func TestNextVersion_FirstVersion(t *testing.T) {
	r := &fakeReader{latestErr: domain.ErrNotFound}
	v, err := NextVersion(context.Background(), r, "user", "u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 1 {
		t.Errorf("expected version 1, got %d", v)
	}
}

func TestNextVersion_Increment(t *testing.T) {
	r := &fakeReader{latest: &domain.VersionRecord{Version: 5}}
	v, err := NextVersion(context.Background(), r, "user", "u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 6 {
		t.Errorf("expected version 6, got %d", v)
	}
}

func TestNextVersion_StorageError(t *testing.T) {
	r := &fakeReader{latestErr: fmt.Errorf("%w: db down", domain.ErrStorage)}
	_, err := NextVersion(context.Background(), r, "user", "u1")
	if err == nil {
		t.Fatal("expected error")
	}
}
