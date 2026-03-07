// Package audit provides a versioning and audit trail library for Go applications.
//
// go-audit automatically captures every change to your domain entities, producing
// a complete, queryable version history with diffs, metadata, and optional
// cryptographic integrity verification.
//
// # Quick Start
//
//	auditor, err := audit.New(audit.Config{
//	    Writer: pgWriter,
//	    Reader: pgReader,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer auditor.Shutdown(context.Background())
//
//	auditor.Register(&User{})
//
//	ctx := audit.WithActor(ctx, "user-123", audit.ActorHuman)
//	result, err := auditor.Version(ctx, &user)
//
// See the examples/ directory for complete working examples.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/abhipray-cpu/go-audit/app"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/integrity"
	"github.com/abhipray-cpu/go-audit/domain/port"
	"github.com/abhipray-cpu/go-audit/domain/reconstruct"
	"github.com/abhipray-cpu/go-audit/domain/registry"
	"github.com/abhipray-cpu/go-audit/domain/version"
)

// Auditor is the central entry point for the go-audit library. It coordinates
// entity registration, version creation, and querying through the configured
// storage, serialization, and diff adapters.
//
// Create an Auditor with [New] and shut it down with [Auditor.Shutdown].
type Auditor struct {
	cfg        Config
	registry   *registry.Registry
	pool       *app.Pool
	migrations *reconstruct.MigrationRegistry
	chain      *integrity.Chain // nil when hash chain is disabled
}

// New creates a fully-wired [Auditor] from the provided [Config].
//
// Required fields: [Config.Writer] and [Config.Reader].
// All other fields default to production-quality implementations:
//   - Logger → slog.Default()
//   - Serializer → JSON
//   - Cloner → reflection deep-copy
//   - Differ → structural diff engine
//
// Returns [domain.ErrConfiguration] if required fields are missing.
func New(cfg Config) (*Auditor, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg.applyDefaults()

	workers := cfg.PoolWorkers
	if workers <= 0 {
		workers = runtime.NumCPU() * 2
	}
	queueSize := cfg.PoolQueueSize
	if queueSize <= 0 {
		queueSize = 10_000
	}

	a := &Auditor{
		cfg:        cfg,
		registry:   registry.New(),
		migrations: reconstruct.NewMigrationRegistry(),
	}

	// Initialize hash chain if enabled.
	if cfg.EnableHashChain && cfg.Hasher != nil {
		a.chain = integrity.NewChain(cfg.Hasher, cfg.Reader)
	}

	a.pool = app.NewPool(app.PoolConfig{
		Workers:   workers,
		QueueSize: queueSize,
		OnBackpressure: func(item app.WorkItem) {
			cfg.Logger.Warn("audit: worker pool backpressure — queue full, blocking until space available")
		},
	})

	return a, nil
}

// Register inspects the given entity type and records its field metadata
// for future versioning operations. The entity must be a struct or pointer
// to struct with exactly one field tagged `version:"id"`.
//
// Register is NOT safe to call concurrently with [Auditor.Version]; call it
// during application initialization before serving traffic.
//
// Returns [domain.ErrConfiguration] if the entity is invalid or already
// registered.
func (a *Auditor) Register(entity any) error {
	return a.registry.Register(entity)
}

// Shutdown performs a graceful shutdown of the Auditor, draining the
// background worker pool. The provided context controls the maximum time
// allowed for shutdown; if exceeded, remaining workers are cancelled.
func (a *Auditor) Shutdown(ctx context.Context) error {
	a.cfg.Logger.Info("audit: shutting down")
	return a.pool.Shutdown(ctx)
}

// Version creates a new immutable version record for the given entity.
//
// By default, Version operates in BackgroundMode: the entity is cloned and
// metadata extracted on the caller's goroutine (~1ms), then diff, serialize,
// and persist run asynchronously in the worker pool. The returned
// [domain.PendingVersion] allows callers to wait for completion if needed.
//
// Use [port.WithSync] to force synchronous execution, which blocks until the
// record is fully persisted before returning.
//
// Options:
//   - [port.WithSync] — force synchronous mode (blocks until persisted)
//   - [port.WithStrategy] — override the storage strategy for this version
//
// Returns [domain.ErrConfiguration] if the entity type is not registered.
// Returns [domain.ErrValidation] if the entity ID is empty.
func (a *Auditor) Version(ctx context.Context, entity any, opts ...port.VersionOption) (*domain.PendingVersion, error) {
	// 1. Resolve options.
	vopts := port.VersionOptions{} // default is BackgroundMode (Sync = false)
	for _, fn := range opts {
		fn(&vopts)
	}

	// 2. Clone entity on the caller's goroutine to isolate from mutations.
	cloned, err := a.cfg.Cloner.Clone(ctx, entity)
	if err != nil {
		return nil, fmt.Errorf("%w: clone failed: %v", domain.ErrDiff, err)
	}

	// 3. Look up entity config from registry (fast, in-memory).
	typeName, err := entityTypeName(cloned)
	if err != nil {
		return nil, err
	}
	ecfg, err := a.registry.Get(typeName)
	if err != nil {
		return nil, err
	}

	// 4. Extract entity ID via reflection.
	entityID, err := extractEntityID(cloned, ecfg)
	if err != nil {
		return nil, err
	}
	if entityID == "" {
		return nil, fmt.Errorf("%w: entity ID must not be empty", domain.ErrValidation)
	}

	// 5. Extract metadata on the caller's goroutine (captures ctx values).
	meta := a.cfg.MetadataExtractor.Extract(ctx)
	meta.Timestamp = a.cfg.Clock.Now()

	// 6. Sync or Background.
	if vopts.Sync {
		record, err := a.versionSync(ctx, cloned, typeName, entityID, ecfg, meta, vopts, "")
		if err != nil {
			return nil, err
		}
		pv := domain.NewPendingVersion()
		pv.Complete(record, nil)
		return pv, nil
	}

	// BackgroundMode: WAL append before enqueue (if configured).
	walEntryID := ""
	if a.cfg.WAL != nil {
		walEntryID = generateID()
		walEntry := port.WALEntry{
			ID: walEntryID,
			Record: domain.VersionRecord{
				EntityType: typeName,
				EntityID:   entityID,
			},
			CreatedAt: a.cfg.Clock.Now(),
		}
		if err := a.cfg.WAL.Append(ctx, walEntry); err != nil {
			return nil, fmt.Errorf("wal append: %w", err)
		}
	}

	// BackgroundMode: enqueue to worker pool.
	pv := domain.NewPendingVersion()
	if !a.pool.Submit(app.WorkItem{
		Process: func(poolCtx context.Context) {
			record, err := a.versionSync(poolCtx, cloned, typeName, entityID, ecfg, meta, vopts, walEntryID)
			pv.Complete(record, err)
		},
	}) {
		pv.Complete(domain.VersionRecord{}, fmt.Errorf("%w: pool is shut down", domain.ErrConfiguration))
	}

	return pv, nil
}

// versionSync is the full sync pipeline: sequence → diff → serialize → persist.
// Used by both BackgroundMode (inside worker) and SyncMode (on caller goroutine).
// walEntryID is the WAL entry to ack after persist; empty if WAL is not in use.
func (a *Auditor) versionSync(
	ctx context.Context,
	cloned any,
	typeName, entityID string,
	ecfg registry.EntityConfig,
	meta domain.VersionMetadata,
	vopts port.VersionOptions,
	walEntryID string,
) (domain.VersionRecord, error) {
	start := a.cfg.Clock.Now()

	// 0a. Authorization hook — BeforeWrite.
	if a.cfg.Hooks != nil {
		if err := a.cfg.Hooks.BeforeWrite(ctx, typeName, entityID); err != nil {
			return domain.VersionRecord{}, fmt.Errorf("%w: %v", domain.ErrValidation, err)
		}
	}

	// 0b. Runtime metadata size guardrail.
	if a.cfg.MaxMetadataSizeBytes > 0 {
		metaJSON, mErr := json.Marshal(meta)
		if mErr == nil && int64(len(metaJSON)) > a.cfg.MaxMetadataSizeBytes {
			a.cfg.Logger.Warn("audit.guardrails.metadata_reject_total",
				"entity_type", typeName,
				"entity_id", entityID,
				"size", int64(len(metaJSON)),
				"limit", a.cfg.MaxMetadataSizeBytes,
			)
			return domain.VersionRecord{}, &domain.MetadataTooLargeError{
				Size:  int64(len(metaJSON)),
				Limit: a.cfg.MaxMetadataSizeBytes,
			}
		}
	}

	// 1. Determine next version number (monotonic per entity).
	nextVer, err := version.NextVersion(ctx, a.cfg.Reader, typeName, entityID)
	if err != nil {
		return domain.VersionRecord{}, fmt.Errorf("version sequence: %w", err)
	}

	// 2. Select storage strategy using the strategy selector.
	strategy := version.SelectStrategy(
		version.StrategyConfig{
			Default:                a.cfg.Strategy,
			HybridSnapshotInterval: a.cfg.HybridSnapshotInterval,
		},
		nextVer,
		vopts.Strategy,
	)

	// 3. Diff against previous version (if strategy requires it).
	var delta domain.Delta
	var prevEntity any
	if nextVer > 1 && strategy != domain.StrategyFull {
		prevRecord, err := a.cfg.Reader.GetLatest(ctx, typeName, entityID)
		if err != nil {
			a.cfg.Logger.Warn("audit: failed to fetch previous version for diff, falling back to full",
				"entity_type", typeName, "entity_id", entityID, "error", err)
			strategy = domain.StrategyFull
		} else {
			prevEntity = reflect.New(ecfg.ReflectType).Interface()
			if uerr := a.cfg.Serializer.Unmarshal(ctx, prevRecord.Data, prevEntity); uerr != nil {
				a.cfg.Logger.Warn("audit: failed to unmarshal previous version, falling back to full",
					"entity_type", typeName, "entity_id", entityID, "error", uerr)
				strategy = domain.StrategyFull
			} else {
				delta, err = a.cfg.Differ.Diff(ctx, prevEntity, cloned)
				if err != nil {
					a.cfg.Logger.Warn("audit: diff failed, falling back to full",
						"entity_type", typeName, "entity_id", entityID, "error", err)
					strategy = domain.StrategyFull
				}
			}
		}
	}

	// 4. Validate delta: Apply(prev, delta) == current.
	if !delta.IsEmpty() && prevEntity != nil {
		if vErr := version.ValidateDelta(ctx, a.cfg.Differ, a.cfg.Serializer, prevEntity, cloned, delta); vErr != nil {
			a.cfg.Logger.Warn("audit: delta validation failed, falling back to full",
				"entity_type", typeName, "entity_id", entityID, "error", vErr)
			strategy = domain.StrategyFull
			delta = domain.Delta{} // clear invalid delta
		}
	}

	// 5. Serialize entity data.
	data, err := a.cfg.Serializer.Marshal(ctx, cloned)
	if err != nil {
		return domain.VersionRecord{}, fmt.Errorf("%w: %v", domain.ErrSerialization, err)
	}

	// 5a. Runtime size guardrail (GA-045).
	if a.cfg.MaxEntitySizeBytes > 0 && int64(len(data)) > a.cfg.MaxEntitySizeBytes {
		a.cfg.Logger.Warn("audit.guardrails.reject_total",
			"entity_type", typeName,
			"entity_id", entityID,
			"size", int64(len(data)),
			"limit", a.cfg.MaxEntitySizeBytes,
		)
		return domain.VersionRecord{}, &domain.EntityTooLargeError{
			EntityType: typeName,
			Size:       int64(len(data)),
			Limit:      a.cfg.MaxEntitySizeBytes,
		}
	}

	// 6. Build the version record.
	record := domain.VersionRecord{
		ID:            generateID(),
		EntityType:    typeName,
		EntityID:      entityID,
		Version:       nextVer,
		Strategy:      strategy,
		SchemaVersion: 1,
		Data:          data,
		ContentType:   a.cfg.Serializer.ContentType(),
		Metadata:      meta,
		CreatedAt:     a.cfg.Clock.Now(),
	}

	if !delta.IsEmpty() {
		record.Delta = &delta
	}

	// 7. Compute hash chain (if enabled).
	if a.chain != nil {
		if err := a.chain.ComputeAndStore(ctx, &record); err != nil {
			a.cfg.Logger.Warn("audit: hash chain computation failed",
				"entity_type", typeName, "entity_id", entityID, "error", err)
			// Non-fatal: persist without hash rather than lose the version.
		}
	}

	// 8. Persist.
	if err := a.cfg.Writer.Save(ctx, record); err != nil {
		return domain.VersionRecord{}, fmt.Errorf("%w: %v", domain.ErrStorage, err)
	}

	// 8a. Notify subscriber (fire-and-forget, errors logged).
	if a.cfg.Subscriber != nil {
		if sErr := a.cfg.Subscriber.OnVersion(ctx, record); sErr != nil {
			a.cfg.Logger.Warn("audit: subscriber notification failed",
				"entity_type", typeName, "entity_id", entityID, "error", sErr)
		}
	}

	// 9. Ack WAL entry after successful persist.
	if walEntryID != "" && a.cfg.WAL != nil {
		if err := a.cfg.WAL.Ack(ctx, walEntryID); err != nil {
			a.cfg.Logger.Warn("audit: WAL ack failed (record already persisted)",
				"wal_entry_id", walEntryID, "error", err)
		}
	}

	a.cfg.Logger.Debug("audit: version created",
		"entity_type", typeName, "entity_id", entityID,
		"version", nextVer, "strategy", strategy.String(),
		"duration", time.Since(start).String())

	return record, nil
}

// VersionInTx creates a new immutable version record within a caller-managed
// database transaction. The record is only visible after the transaction
// commits. This call is always synchronous.
//
// Returns [domain.ErrValidation] if tx is nil.
// Returns [domain.ErrConfiguration] if the entity type is not registered.
func (a *Auditor) VersionInTx(ctx context.Context, tx port.Transaction, entity any, opts ...port.VersionOption) (domain.VersionResult, error) {
	if tx == nil {
		return domain.VersionResult{}, fmt.Errorf("%w: transaction must not be nil", domain.ErrValidation)
	}

	start := a.cfg.Clock.Now()

	// 1. Clone entity to isolate from caller mutations.
	cloned, err := a.cfg.Cloner.Clone(ctx, entity)
	if err != nil {
		return domain.VersionResult{}, fmt.Errorf("%w: clone failed: %v", domain.ErrDiff, err)
	}

	// 2. Look up entity config from registry.
	typeName, err := entityTypeName(cloned)
	if err != nil {
		return domain.VersionResult{}, err
	}
	ecfg, err := a.registry.Get(typeName)
	if err != nil {
		return domain.VersionResult{}, err
	}

	// 3. Extract entity ID.
	entityID, err := extractEntityID(cloned, ecfg)
	if err != nil {
		return domain.VersionResult{}, err
	}
	if entityID == "" {
		return domain.VersionResult{}, fmt.Errorf("%w: entity ID must not be empty", domain.ErrValidation)
	}

	// 4. Extract metadata.
	meta := a.cfg.MetadataExtractor.Extract(ctx)
	meta.Timestamp = a.cfg.Clock.Now()

	// 4a. Authorization hook — BeforeWrite.
	if a.cfg.Hooks != nil {
		if err := a.cfg.Hooks.BeforeWrite(ctx, typeName, entityID); err != nil {
			return domain.VersionResult{}, fmt.Errorf("%w: %v", domain.ErrValidation, err)
		}
	}

	// 4b. Runtime metadata size guardrail.
	if a.cfg.MaxMetadataSizeBytes > 0 {
		metaJSON, mErr := json.Marshal(meta)
		if mErr == nil && int64(len(metaJSON)) > a.cfg.MaxMetadataSizeBytes {
			a.cfg.Logger.Warn("audit.guardrails.metadata_reject_total",
				"entity_type", typeName,
				"entity_id", entityID,
				"size", int64(len(metaJSON)),
				"limit", a.cfg.MaxMetadataSizeBytes,
			)
			return domain.VersionResult{}, &domain.MetadataTooLargeError{
				Size:  int64(len(metaJSON)),
				Limit: a.cfg.MaxMetadataSizeBytes,
			}
		}
	}

	// 5. Resolve options (always sync for tx).
	vopts := port.VersionOptions{Sync: true}
	for _, fn := range opts {
		fn(&vopts)
	}

	// 6. Determine next version number.
	nextVer, err := version.NextVersion(ctx, a.cfg.Reader, typeName, entityID)
	if err != nil {
		return domain.VersionResult{}, fmt.Errorf("version sequence: %w", err)
	}

	// 7. Select storage strategy.
	strategy := version.SelectStrategy(
		version.StrategyConfig{
			Default:                a.cfg.Strategy,
			HybridSnapshotInterval: a.cfg.HybridSnapshotInterval,
		},
		nextVer,
		vopts.Strategy,
	)

	// 8. Diff against previous version (if strategy requires it).
	var delta domain.Delta
	if nextVer > 1 && strategy != domain.StrategyFull {
		prevRecord, err := a.cfg.Reader.GetLatest(ctx, typeName, entityID)
		if err != nil {
			a.cfg.Logger.Warn("audit: failed to fetch previous version for diff, falling back to full",
				"entity_type", typeName, "entity_id", entityID, "error", err)
			strategy = domain.StrategyFull
		} else {
			prevEntity := reflect.New(ecfg.ReflectType).Interface()
			if uerr := a.cfg.Serializer.Unmarshal(ctx, prevRecord.Data, prevEntity); uerr != nil {
				a.cfg.Logger.Warn("audit: failed to unmarshal previous version, falling back to full",
					"entity_type", typeName, "entity_id", entityID, "error", uerr)
				strategy = domain.StrategyFull
			} else {
				delta, err = a.cfg.Differ.Diff(ctx, prevEntity, cloned)
				if err != nil {
					a.cfg.Logger.Warn("audit: diff failed, falling back to full",
						"entity_type", typeName, "entity_id", entityID, "error", err)
					strategy = domain.StrategyFull
				}
			}
		}
	}

	// 9. Serialize entity data.
	data, err := a.cfg.Serializer.Marshal(ctx, cloned)
	if err != nil {
		return domain.VersionResult{}, fmt.Errorf("%w: %v", domain.ErrSerialization, err)
	}

	// 9a. Runtime size guardrail (GA-045).
	if a.cfg.MaxEntitySizeBytes > 0 && int64(len(data)) > a.cfg.MaxEntitySizeBytes {
		a.cfg.Logger.Warn("audit.guardrails.reject_total",
			"entity_type", typeName,
			"entity_id", entityID,
			"size", int64(len(data)),
			"limit", a.cfg.MaxEntitySizeBytes,
		)
		return domain.VersionResult{}, &domain.EntityTooLargeError{
			EntityType: typeName,
			Size:       int64(len(data)),
			Limit:      a.cfg.MaxEntitySizeBytes,
		}
	}

	// 10. Build the version record.
	record := domain.VersionRecord{
		ID:            generateID(),
		EntityType:    typeName,
		EntityID:      entityID,
		Version:       nextVer,
		Strategy:      strategy,
		SchemaVersion: 1,
		Data:          data,
		ContentType:   a.cfg.Serializer.ContentType(),
		Metadata:      meta,
		CreatedAt:     a.cfg.Clock.Now(),
	}

	if !delta.IsEmpty() {
		record.Delta = &delta
	}

	// 11. Compute hash chain (if enabled).
	if a.chain != nil {
		if err := a.chain.ComputeAndStore(ctx, &record); err != nil {
			a.cfg.Logger.Warn("audit: hash chain computation failed",
				"entity_type", typeName, "entity_id", entityID, "error", err)
		}
	}

	// 12. Persist within caller's transaction.
	if err := a.cfg.Writer.SaveInTx(ctx, tx, record); err != nil {
		return domain.VersionResult{}, fmt.Errorf("%w: %v", domain.ErrStorage, err)
	}

	// 12a. Notify subscriber (fire-and-forget, errors logged).
	if a.cfg.Subscriber != nil {
		if sErr := a.cfg.Subscriber.OnVersion(ctx, record); sErr != nil {
			a.cfg.Logger.Warn("audit: subscriber notification failed",
				"entity_type", typeName, "entity_id", entityID, "error", sErr)
		}
	}

	a.cfg.Logger.Debug("audit: version created (in tx)",
		"entity_type", typeName, "entity_id", entityID,
		"version", nextVer, "strategy", strategy.String())

	return domain.VersionResult{
		Record:   record,
		Duration: time.Since(start),
		Strategy: strategy,
	}, nil
}

// extractEntityID reads the ID field value from the entity using the
// registry's field metadata.
func extractEntityID(entity any, ecfg registry.EntityConfig) (string, error) {
	v := reflect.ValueOf(entity)
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", fmt.Errorf("%w: entity must be a struct", domain.ErrConfiguration)
	}

	idField := v.FieldByName(ecfg.IDField)
	if !idField.IsValid() {
		return "", fmt.Errorf("%w: ID field %q not found on entity", domain.ErrConfiguration, ecfg.IDField)
	}

	return fmt.Sprintf("%v", idField.Interface()), nil
}

// generateID produces a time-sortable unique ID.
// Uses a simple timestamp+counter approach; will be replaced with ULID in GA-050.
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// Writer returns the configured VersionWriterPort. This is exposed for
// advanced usage and testing; most callers should use [Auditor.Version].
func (a *Auditor) Writer() port.VersionWriterPort { return a.cfg.Writer }

// Reader returns the configured VersionReaderPort. This is exposed for
// advanced usage and testing; most callers should use the query methods.
func (a *Auditor) Reader() port.VersionReaderPort { return a.cfg.Reader }

// Registry returns the entity registry. Exported for advanced usage only.
func (a *Auditor) Registry() *registry.Registry { return a.registry }

// Config returns a copy of the resolved (defaults-applied) configuration.
// Useful for debugging and testing.
func (a *Auditor) Config() Config { return a.cfg }

// ---------- Query API (GA-024) ----------

// GetVersion retrieves a specific version of an entity by its version number.
// Returns [domain.ErrNotFound] if the record does not exist.
func (a *Auditor) GetVersion(ctx context.Context, entityType, entityID string, ver int64) (domain.VersionRecord, error) {
	if a.cfg.Hooks != nil {
		if err := a.cfg.Hooks.BeforeRead(ctx, entityType, entityID); err != nil {
			return domain.VersionRecord{}, fmt.Errorf("%w: %v", domain.ErrValidation, err)
		}
	}
	return a.cfg.Reader.GetByVersion(ctx, entityType, entityID, ver)
}

// GetLatest retrieves the most recent version of an entity.
// Returns [domain.ErrNotFound] if the entity has no versions.
func (a *Auditor) GetLatest(ctx context.Context, entityType, entityID string) (domain.VersionRecord, error) {
	if a.cfg.Hooks != nil {
		if err := a.cfg.Hooks.BeforeRead(ctx, entityType, entityID); err != nil {
			return domain.VersionRecord{}, fmt.Errorf("%w: %v", domain.ErrValidation, err)
		}
	}
	return a.cfg.Reader.GetLatest(ctx, entityType, entityID)
}

// GetAtTime retrieves the version of an entity that was current at the given
// point in time.
// Returns [domain.ErrNotFound] if no version existed at that time.
func (a *Auditor) GetAtTime(ctx context.Context, entityType, entityID string, t time.Time) (domain.VersionRecord, error) {
	if a.cfg.Hooks != nil {
		if err := a.cfg.Hooks.BeforeRead(ctx, entityType, entityID); err != nil {
			return domain.VersionRecord{}, fmt.Errorf("%w: %v", domain.ErrValidation, err)
		}
	}
	return a.cfg.Reader.GetAtTime(ctx, entityType, entityID, t)
}

// ListVersions returns a paginated list of version records for an entity,
// ordered by version number. Use [port.WithLimit], [port.WithOffset],
// [port.WithOrderAsc], and [port.WithOrderDesc] to control pagination.
func (a *Auditor) ListVersions(ctx context.Context, entityType, entityID string, opts ...port.ListOption) ([]domain.VersionRecord, error) {
	if a.cfg.Hooks != nil {
		if err := a.cfg.Hooks.BeforeRead(ctx, entityType, entityID); err != nil {
			return nil, fmt.Errorf("%w: %v", domain.ErrValidation, err)
		}
	}
	return a.cfg.Reader.ListVersions(ctx, entityType, entityID, opts...)
}

// ---------- Advanced Query API (GA-046) ----------

// Compare computes the delta between two specific versions of an entity.
// Returns a [domain.Delta] describing the field-level changes from v1 to v2.
// Both versions must exist; returns [domain.ErrNotFound] otherwise.
func (a *Auditor) Compare(ctx context.Context, entityType, entityID string, v1, v2 int64) (domain.Delta, error) {
	rec1, err := a.cfg.Reader.GetByVersion(ctx, entityType, entityID, v1)
	if err != nil {
		return domain.Delta{}, fmt.Errorf("version %d: %w", v1, err)
	}
	rec2, err := a.cfg.Reader.GetByVersion(ctx, entityType, entityID, v2)
	if err != nil {
		return domain.Delta{}, fmt.Errorf("version %d: %w", v2, err)
	}

	ecfg, err := a.registry.Get(entityType)
	if err != nil {
		return domain.Delta{}, err
	}

	entity1 := reflect.New(ecfg.ReflectType).Interface()
	if err := a.cfg.Serializer.Unmarshal(ctx, rec1.Data, entity1); err != nil {
		return domain.Delta{}, fmt.Errorf("%w: unmarshal v%d: %v", domain.ErrSerialization, v1, err)
	}

	entity2 := reflect.New(ecfg.ReflectType).Interface()
	if err := a.cfg.Serializer.Unmarshal(ctx, rec2.Data, entity2); err != nil {
		return domain.Delta{}, fmt.Errorf("%w: unmarshal v%d: %v", domain.ErrSerialization, v2, err)
	}

	delta, err := a.cfg.Differ.Diff(ctx, entity1, entity2)
	if err != nil {
		return domain.Delta{}, fmt.Errorf("%w: diff v%d→v%d: %v", domain.ErrDiff, v1, v2, err)
	}
	return delta, nil
}

// FindChanges returns all field-level changes to a specific field across
// the version history of an entity. The fieldPath uses dotted notation
// (e.g., "Name", "Address.City").
func (a *Auditor) FindChanges(ctx context.Context, entityType, entityID, fieldPath string, opts ...port.ListOption) ([]domain.FieldChange, error) {
	records, err := a.cfg.Reader.ListVersions(ctx, entityType, entityID, port.WithOrderAsc())
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, nil // no changes possible with 0 or 1 versions
	}

	ecfg, err := a.registry.Get(entityType)
	if err != nil {
		return nil, err
	}

	var changes []domain.FieldChange

	for i := 1; i < len(records); i++ {
		prev := records[i-1]
		curr := records[i]

		entity1 := reflect.New(ecfg.ReflectType).Interface()
		if err := a.cfg.Serializer.Unmarshal(ctx, prev.Data, entity1); err != nil {
			continue
		}
		entity2 := reflect.New(ecfg.ReflectType).Interface()
		if err := a.cfg.Serializer.Unmarshal(ctx, curr.Data, entity2); err != nil {
			continue
		}

		delta, err := a.cfg.Differ.Diff(ctx, entity1, entity2)
		if err != nil {
			continue
		}

		for _, fc := range delta.Changes {
			if fc.Path == fieldPath {
				changes = append(changes, fc)
			}
		}
	}

	return changes, nil
}

// FindByActor returns all version records created by the given actor ID.
// Uses client-side filtering over [ListVersions]; for large datasets use
// a backend-specific query.
func (a *Auditor) FindByActor(ctx context.Context, entityType, entityID, actorID string, opts ...port.ListOption) ([]domain.VersionRecord, error) {
	records, err := a.cfg.Reader.ListVersions(ctx, entityType, entityID, opts...)
	if err != nil {
		return nil, err
	}

	var matched []domain.VersionRecord
	for _, r := range records {
		if r.Metadata.ActorID == actorID {
			matched = append(matched, r)
		}
	}
	return matched, nil
}

// GetBulkAtTime retrieves the version of multiple entities at a specific
// point in time. Returns one record per entity ID (in the same order).
// Entities with no version at that time are omitted from the result.
func (a *Auditor) GetBulkAtTime(ctx context.Context, entityType string, entityIDs []string, t time.Time) ([]domain.VersionRecord, error) {
	results := make([]domain.VersionRecord, 0, len(entityIDs))
	for _, eid := range entityIDs {
		rec, err := a.cfg.Reader.GetAtTime(ctx, entityType, eid, t)
		if err != nil {
			continue // skip entities with no version at that time
		}
		results = append(results, rec)
	}
	return results, nil
}

// LookupEntity returns the [registry.EntityConfig] for the given entity
// instance, or [domain.ErrConfiguration] if the entity type is not
// registered.
func (a *Auditor) LookupEntity(entity any) (registry.EntityConfig, error) {
	typeName, err := entityTypeName(entity)
	if err != nil {
		return registry.EntityConfig{}, err
	}
	return a.registry.Get(typeName)
}

// ---------- Schema Evolution API (GA-050) ----------

// RegisterMigration registers a migration function for lazy on-read
// schema evolution. fromVersion and toVersion must be consecutive
// (toVersion == fromVersion + 1). Migrations are applied automatically
// when reading older version records.
//
// Returns [domain.ErrMigration] if there is a gap or duplicate.
func (a *Auditor) RegisterMigration(entityType string, fromVersion, toVersion int, fn port.MigrationFunc) error {
	return a.migrations.Register(entityType, fromVersion, toVersion, fn)
}

// Migrations returns the migration registry for advanced usage and testing.
func (a *Auditor) Migrations() *reconstruct.MigrationRegistry {
	return a.migrations
}

// ---------- Integrity Verification API (GA-051) ----------

// VerifyIntegrity checks the hash chain for the given entity. Returns nil
// if the chain is valid, or an error if tampering is detected.
// Returns [domain.ErrConfiguration] if hash chain is not enabled.
func (a *Auditor) VerifyIntegrity(ctx context.Context, entityType, entityID string) error {
	if a.chain == nil {
		return fmt.Errorf("%w: hash chain is not enabled (set Config.EnableHashChain = true)", domain.ErrConfiguration)
	}
	return a.chain.VerifyIntegrity(ctx, entityType, entityID)
}

// ---------- Field Redaction API (GA-053) ----------

// RedactField replaces the value of the specified field with "[REDACTED]"
// in all stored versions of the entity. This is a destructive, GDPR-
// compliant operation that preserves the audit trail structure while
// removing PII.
//
// The fieldPath uses dotted notation (e.g., "Email", "Address.Street").
// Only the serialized data is modified; the version metadata and delta
// structure are preserved.
//
// Returns the number of versions redacted, or an error.
func (a *Auditor) RedactField(ctx context.Context, entityType, entityID, fieldPath string) (int, error) {
	records, err := a.cfg.Reader.ListVersions(ctx, entityType, entityID, port.WithOrderAsc())
	if err != nil {
		return 0, fmt.Errorf("%w: list versions for redaction: %v", domain.ErrStorage, err)
	}

	count := 0
	for _, rec := range records {
		redacted, err := domain.RedactField(rec.Data, fieldPath)
		if err != nil {
			a.cfg.Logger.Warn("audit: redaction failed for version",
				"entity_type", entityType, "entity_id", entityID,
				"version", rec.Version, "error", err)
			continue
		}

		// Only re-save if data actually changed.
		if string(redacted) == string(rec.Data) {
			continue
		}

		rec.Data = redacted
		if err := a.cfg.Writer.Update(ctx, rec); err != nil {
			return count, fmt.Errorf("%w: re-save redacted version %d: %v", domain.ErrStorage, rec.Version, err)
		}
		count++
	}

	return count, nil
}

// entityTypeName extracts the lowercased struct name from an entity value.
func entityTypeName(entity any) (string, error) {
	if entity == nil {
		return "", fmt.Errorf("%w: entity must not be nil", domain.ErrConfiguration)
	}

	t := reflect.TypeOf(entity)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return "", fmt.Errorf("%w: entity must be a struct, got %s", domain.ErrConfiguration, t.Kind())
	}

	name := strings.ToLower(t.Name())
	if name == "" {
		return "", fmt.Errorf("%w: anonymous structs are not supported", domain.ErrConfiguration)
	}
	return name, nil
}
