package app

import (
	"context"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// StorageRouter routes version record writes to both an OLTP backend
// (synchronous) and an optional OLAP backend (asynchronous via batch
// buffer). Reads are tiered: recent data from OLTP, historical from OLAP.
type StorageRouter struct {
	oltp port.VersionWriterPort
	olap port.OLAPPort // nil when OLAP is not configured
}

// RouterConfig configures a [StorageRouter].
type RouterConfig struct {
	// OLTP is the primary synchronous storage writer (required).
	OLTP port.VersionWriterPort

	// OLAP is the optional analytical backend for async batch writes.
	// When nil, writes go to OLTP only.
	OLAP port.OLAPPort
}

// NewStorageRouter creates a [StorageRouter].
func NewStorageRouter(cfg RouterConfig) *StorageRouter {
	return &StorageRouter{
		oltp: cfg.OLTP,
		olap: cfg.OLAP,
	}
}

// Save persists the record to the OLTP backend synchronously and, if an
// OLAP backend is configured, enqueues a batch insert of one record
// asynchronously. OLAP failures are isolated — they do not affect the
// OLTP write.
func (r *StorageRouter) Save(ctx context.Context, record domain.VersionRecord) error {
	// 1. Sync write to OLTP.
	if err := r.oltp.Save(ctx, record); err != nil {
		return err
	}

	// 2. Async enqueue to OLAP (fire-and-forget).
	if r.olap != nil {
		go func() {
			_ = r.olap.BatchInsert(context.Background(), []domain.VersionRecord{record})
		}()
	}

	return nil
}

// Update delegates to the OLTP backend's Update method for in-place
// modifications (e.g., redaction). OLAP is not updated — redaction of
// OLAP records should be handled separately.
func (r *StorageRouter) Update(ctx context.Context, record domain.VersionRecord) error {
	return r.oltp.Update(ctx, record)
}

// SaveInTx delegates to the OLTP backend's transactional save. OLAP
// enqueue happens outside the transaction scope.
func (r *StorageRouter) SaveInTx(ctx context.Context, tx port.Transaction, record domain.VersionRecord) error {
	if err := r.oltp.SaveInTx(ctx, tx, record); err != nil {
		return err
	}

	if r.olap != nil {
		go func() {
			_ = r.olap.BatchInsert(context.Background(), []domain.VersionRecord{record})
		}()
	}

	return nil
}

// ReadFromOLTP reads a record from the primary OLTP backend.
func (r *StorageRouter) ReadFromOLTP(ctx context.Context, reader port.VersionReaderPort, entityType, entityID string, ver int64) (domain.VersionRecord, error) {
	return reader.GetByVersion(ctx, entityType, entityID, ver)
}

// ReadFromOLAP queries the OLAP backend for analytical/historical data.
func (r *StorageRouter) ReadFromOLAP(ctx context.Context, entityType string, opts ...port.ListOption) ([]domain.VersionRecord, error) {
	if r.olap == nil {
		return nil, nil
	}
	return r.olap.Query(ctx, entityType, opts...)
}
