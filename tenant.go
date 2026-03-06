package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Tenant context key — private to prevent collisions.
type tenantKeyType int

const tenantKey tenantKeyType = iota

// WithTenant attaches a tenant ID to the context. When set, the tenant ID
// is automatically included in version metadata and used for query isolation.
//
//	ctx = audit.WithTenant(ctx, "tenant-abc")
func WithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantKey, tenantID)
}

// TenantFromCtx extracts the tenant ID from the context, or "" if not set.
func TenantFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(tenantKey).(string)
	return v
}

// TenantScopedReader wraps a [port.VersionReaderPort] and filters results
// to only return records belonging to the tenant in the context. Records
// without a matching tenant are hidden.
type TenantScopedReader struct {
	inner port.VersionReaderPort
}

// NewTenantScopedReader creates a tenant-scoped reader.
func NewTenantScopedReader(inner port.VersionReaderPort) *TenantScopedReader {
	return &TenantScopedReader{inner: inner}
}

// GetByVersion returns the record only if it belongs to the context tenant.
func (r *TenantScopedReader) GetByVersion(ctx context.Context, entityType, entityID string, version int64) (domain.VersionRecord, error) {
	rec, err := r.inner.GetByVersion(ctx, entityType, entityID, version)
	if err != nil {
		return rec, err
	}
	if err := r.checkTenant(ctx, rec); err != nil {
		return domain.VersionRecord{}, err
	}
	return rec, nil
}

// GetLatest returns the latest record only if it belongs to the context tenant.
func (r *TenantScopedReader) GetLatest(ctx context.Context, entityType, entityID string) (domain.VersionRecord, error) {
	rec, err := r.inner.GetLatest(ctx, entityType, entityID)
	if err != nil {
		return rec, err
	}
	if err := r.checkTenant(ctx, rec); err != nil {
		return domain.VersionRecord{}, err
	}
	return rec, nil
}

// GetAtTime returns the record at time only if it belongs to the context tenant.
func (r *TenantScopedReader) GetAtTime(ctx context.Context, entityType, entityID string, t time.Time) (domain.VersionRecord, error) {
	rec, err := r.inner.GetAtTime(ctx, entityType, entityID, t)
	if err != nil {
		return rec, err
	}
	if err := r.checkTenant(ctx, rec); err != nil {
		return domain.VersionRecord{}, err
	}
	return rec, nil
}

// ListVersions returns only records belonging to the context tenant.
func (r *TenantScopedReader) ListVersions(ctx context.Context, entityType, entityID string, opts ...port.ListOption) ([]domain.VersionRecord, error) {
	recs, err := r.inner.ListVersions(ctx, entityType, entityID, opts...)
	if err != nil {
		return nil, err
	}
	tenant := TenantFromCtx(ctx)
	if tenant == "" {
		return recs, nil
	}
	var filtered []domain.VersionRecord
	for _, rec := range recs {
		if rec.Metadata.Custom["tenant_id"] == tenant {
			filtered = append(filtered, rec)
		}
	}
	return filtered, nil
}

// checkTenant verifies a record belongs to the context tenant. Returns
// [domain.ErrNotFound] if there is a mismatch (tenant A can't see tenant B).
func (r *TenantScopedReader) checkTenant(ctx context.Context, rec domain.VersionRecord) error {
	tenant := TenantFromCtx(ctx)
	if tenant == "" {
		return nil // no tenant in context → no filtering
	}
	if rec.Metadata.Custom["tenant_id"] != tenant {
		return fmt.Errorf("%w: tenant isolation", domain.ErrNotFound)
	}
	return nil
}
