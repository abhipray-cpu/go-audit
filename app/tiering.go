package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// TierName identifies a storage tier.
type TierName string

const (
	// TierHot is the primary OLTP store (<90 days by default).
	TierHot TierName = "hot"

	// TierWarm is the OLAP analytics store (90 days–2 years by default).
	TierWarm TierName = "warm"

	// TierCold is the compressed cold store (>2 years by default).
	TierCold TierName = "cold"
)

// TieringConfig configures the background cold-tiering engine.
type TieringConfig struct {
	// WarmAfterDays is the age in days after which records move to warm
	// (OLAP). Default: 90.
	WarmAfterDays int

	// ColdAfterDays is the age in days after which records move to cold
	// (S3/GCS). Default: 730 (2 years).
	ColdAfterDays int

	// EntityTypes is the list of entity types to tier. If empty, all
	// entity types returned by the reader are tiered.
	EntityTypes []string

	// Reader is the primary OLTP reader for listing version records.
	Reader port.VersionReaderPort

	// OLAP is the warm-tier OLAP writer.
	OLAP port.OLAPPort

	// ColdStore is the cold-tier writer (S3, GCS, etc.).
	ColdStore port.ColdStorePort

	// Serializer marshals version records for cold storage.
	Serializer port.SerializerPort

	// Logger for status messages.
	Logger port.LoggerPort

	// Interval between tiering runs. Default: 1 hour.
	Interval time.Duration
}

// TieringResult holds the result of a single tiering pass.
type TieringResult struct {
	MovedToWarm int
	MovedToCold int
}

// TieringEngine runs periodic background archival of version records
// from hot (OLTP) → warm (OLAP) → cold (S3/GCS).
type TieringEngine struct {
	cfg    TieringConfig
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewTieringEngine creates and starts a background tiering engine.
// Call Stop() to shut it down.
func NewTieringEngine(cfg TieringConfig) *TieringEngine {
	if cfg.WarmAfterDays <= 0 {
		cfg.WarmAfterDays = 90
	}
	if cfg.ColdAfterDays <= 0 {
		cfg.ColdAfterDays = 730
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 1 * time.Hour
	}

	ctx, cancel := context.WithCancel(context.Background())
	e := &TieringEngine{
		cfg:    cfg,
		cancel: cancel,
	}

	e.wg.Add(1)
	go e.loop(ctx)

	return e
}

// Stop shuts down the background tiering engine and waits for the
// current cycle to finish.
func (e *TieringEngine) Stop() {
	e.cancel()
	e.wg.Wait()
}

// RunOnce executes a single tiering pass. Useful for testing without
// starting the background loop.
func (e *TieringEngine) RunOnce(ctx context.Context) TieringResult {
	var result TieringResult
	now := time.Now()
	warmCutoff := now.AddDate(0, 0, -e.cfg.WarmAfterDays)
	coldCutoff := now.AddDate(0, 0, -e.cfg.ColdAfterDays)

	for _, entityType := range e.cfg.EntityTypes {
		records, err := e.cfg.Reader.ListVersions(ctx, entityType, "")
		if err != nil {
			if e.cfg.Logger != nil {
				e.cfg.Logger.Warn("tiering: list versions failed",
					"entity_type", entityType, "error", err)
			}
			continue
		}

		for _, rec := range records {
			if rec.CreatedAt.Before(coldCutoff) {
				// Move to cold storage.
				if e.cfg.ColdStore != nil {
					key := coldStoreKey(rec)
					if err := e.cfg.ColdStore.Put(ctx, key, rec.Data); err != nil {
						if e.cfg.Logger != nil {
							e.cfg.Logger.Warn("tiering: cold store put failed",
								"entity_type", entityType, "key", key, "error", err)
						}
						continue
					}
					result.MovedToCold++
				}
			} else if rec.CreatedAt.Before(warmCutoff) {
				// Move to warm (OLAP).
				if e.cfg.OLAP != nil {
					if err := e.cfg.OLAP.BatchInsert(ctx, []domain.VersionRecord{rec}); err != nil {
						if e.cfg.Logger != nil {
							e.cfg.Logger.Warn("tiering: OLAP insert failed",
								"entity_type", entityType, "error", err)
						}
						continue
					}
					result.MovedToWarm++
				}
			}
		}
	}

	return result
}

func (e *TieringEngine) loop(ctx context.Context) {
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

// coldStoreKey builds a deterministic key for cold storage.
func coldStoreKey(rec domain.VersionRecord) string {
	return fmt.Sprintf("%s/%s/%d", rec.EntityType, rec.EntityID, rec.Version)
}
