package app

import (
	"context"
	"sync"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// RetentionConfig configures the background retention cleanup.
type RetentionConfig struct {
	// RetentionDays maps entity type to retention days. Versions older
	// than the retention period are eligible for deletion.
	RetentionDays map[string]int

	// Reader reads version records for retention scanning.
	Reader port.VersionReaderPort

	// Deleter deletes expired version records. If the writer implements
	// this interface, it is used for cleanup.
	Deleter VersionDeleterPort

	// Logger for status messages.
	Logger port.LoggerPort

	// Interval between cleanup runs. Default: 1 hour.
	Interval time.Duration
}

// VersionDeleterPort deletes version records. Storage adapters implement
// this interface to support retention cleanup.
type VersionDeleterPort interface {
	// DeleteBefore deletes all versions of the given entity type that were
	// created before the cutoff time.
	DeleteBefore(ctx context.Context, entityType string, before time.Time) (int, error)
}

// RetentionEngine runs periodic background cleanup of expired version records.
type RetentionEngine struct {
	cfg    RetentionConfig
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewRetentionEngine creates and starts a background retention engine.
// Call Stop() to shut it down.
func NewRetentionEngine(cfg RetentionConfig) *RetentionEngine {
	if cfg.Interval <= 0 {
		cfg.Interval = 1 * time.Hour
	}

	ctx, cancel := context.WithCancel(context.Background())
	e := &RetentionEngine{
		cfg:    cfg,
		cancel: cancel,
	}

	e.wg.Add(1)
	go e.loop(ctx)

	return e
}

// Stop shuts down the background retention engine and waits for the
// current cleanup cycle to finish.
func (e *RetentionEngine) Stop() {
	e.cancel()
	e.wg.Wait()
}

// RunOnce executes a single retention cleanup pass. Useful for testing
// without starting the background loop.
func (e *RetentionEngine) RunOnce(ctx context.Context) map[string]int {
	results := make(map[string]int)
	now := time.Now()

	for entityType, days := range e.cfg.RetentionDays {
		if days <= 0 {
			continue
		}
		cutoff := now.AddDate(0, 0, -days)
		deleted, err := e.cfg.Deleter.DeleteBefore(ctx, entityType, cutoff)
		if err != nil {
			if e.cfg.Logger != nil {
				e.cfg.Logger.Warn("retention: cleanup failed",
					"entity_type", entityType, "error", err)
			}
			continue
		}
		results[entityType] = deleted
		if deleted > 0 && e.cfg.Logger != nil {
			e.cfg.Logger.Info("retention: cleaned up expired versions",
				"entity_type", entityType, "deleted", deleted, "cutoff", cutoff.String())
		}
	}

	return results
}

func (e *RetentionEngine) loop(ctx context.Context) {
	defer e.wg.Done()

	ticker := time.NewTicker(e.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.RunOnce(ctx)
		}
	}
}

// ---------- in-memory deleter for testing ----------

// MemoryDeleter is an in-memory [VersionDeleterPort] for testing retention.
type MemoryDeleter struct {
	mu      sync.Mutex
	records *[]domain.VersionRecord
}

// NewMemoryDeleter wraps a slice of records for in-memory deletion.
func NewMemoryDeleter(records *[]domain.VersionRecord) *MemoryDeleter {
	return &MemoryDeleter{records: records}
}

// DeleteBefore removes all records of entityType created before the cutoff.
func (d *MemoryDeleter) DeleteBefore(_ context.Context, entityType string, before time.Time) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var kept []domain.VersionRecord
	deleted := 0
	for _, r := range *d.records {
		if r.EntityType == entityType && r.CreatedAt.Before(before) {
			deleted++
		} else {
			kept = append(kept, r)
		}
	}
	*d.records = kept
	return deleted, nil
}
