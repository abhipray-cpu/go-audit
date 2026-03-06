package reconstruct

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
	"github.com/abhipray-cpu/go-audit/domain/registry"
)

// DefaultMaxDeltaChain is the default maximum number of consecutive deltas
// before a compaction snapshot is inserted. Longer chains degrade
// reconstruction performance.
const DefaultMaxDeltaChain = 50

// Compactor bounds delta chain length by inserting synthetic snapshot
// records when the chain exceeds [MaxDeltaChain]. This runs as a
// background maintenance task.
type Compactor struct {
	reader     port.VersionReaderPort
	writer     port.VersionWriterPort
	serializer port.SerializerPort
	differ     port.DifferPort
	registry   *registry.Registry
	engine     *Engine

	// MaxDeltaChain is the maximum number of consecutive delta records before
	// a compaction snapshot is generated. Default: 50.
	MaxDeltaChain int
}

// CompactorConfig configures a [Compactor].
type CompactorConfig struct {
	Reader     port.VersionReaderPort
	Writer     port.VersionWriterPort
	Serializer port.SerializerPort
	Differ     port.DifferPort
	Registry   *registry.Registry

	// MaxDeltaChain is the maximum consecutive deltas before inserting a
	// compaction snapshot. Default: [DefaultMaxDeltaChain] (50).
	MaxDeltaChain int
}

// NewCompactor creates a [Compactor] with the given configuration.
func NewCompactor(cfg CompactorConfig) *Compactor {
	maxChain := cfg.MaxDeltaChain
	if maxChain <= 0 {
		maxChain = DefaultMaxDeltaChain
	}

	engine := NewEngine(cfg.Reader, cfg.Serializer, cfg.Differ, cfg.Registry)

	return &Compactor{
		reader:        cfg.Reader,
		writer:        cfg.Writer,
		serializer:    cfg.Serializer,
		differ:        cfg.Differ,
		registry:      cfg.Registry,
		engine:        engine,
		MaxDeltaChain: maxChain,
	}
}

// Compact inspects the version history for the given entity and inserts
// compaction snapshots wherever the delta chain exceeds [Compactor.MaxDeltaChain].
//
// Returns the number of compaction snapshots inserted, or an error.
func (c *Compactor) Compact(ctx context.Context, entityType, entityID string) (int, error) {
	// 1. Fetch all versions, ordered ascending.
	records, err := c.reader.ListVersions(ctx, entityType, entityID, port.WithOrderAsc())
	if err != nil {
		return 0, fmt.Errorf("%w: list versions: %v", domain.ErrStorage, err)
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].Version < records[j].Version
	})

	// 2. Walk through records, tracking delta chain length.
	compacted := 0
	chainLen := 0

	for _, r := range records {
		if r.Strategy == domain.StrategyFull {
			chainLen = 0
			continue
		}

		// Delta record.
		chainLen++

		if chainLen >= c.MaxDeltaChain {
			// 3. Look up the entity config for type info.
			ecfg, err := c.registry.Get(entityType)
			if err != nil {
				return compacted, err
			}

			// 4. Unmarshal entity from record data to extract the entity ID.
			//    Data always contains the full serialized entity regardless of
			//    the storage strategy, so we can use it directly rather than
			//    reconstructing from the delta chain.
			entity := reflect.New(ecfg.ReflectType).Interface()
			if err := c.serializer.Unmarshal(ctx, r.Data, entity); err != nil {
				return compacted, fmt.Errorf("%w: compaction unmarshal v%d: %v", domain.ErrSerialization, r.Version, err)
			}

			// 5. Extract entity ID from the deserialized entity.
			eid, err := extractEntityIDFromReconstructed(entity, ecfg)
			if err != nil {
				return compacted, err
			}

			// 6. Write a compaction snapshot record.
			// We overwrite the existing record's strategy to convert it from
			// delta to full. Data is already the full entity — only the
			// strategy marker needs to change.
			compactionRecord := domain.VersionRecord{
				ID:            r.ID, // keep same ID
				EntityType:    entityType,
				EntityID:      eid,
				Version:       r.Version,
				Strategy:      domain.StrategyFull,
				SchemaVersion: r.SchemaVersion,
				Data:          r.Data, // already the full entity
				ContentType:   r.ContentType,
				Metadata:      r.Metadata,
				CreatedAt:     r.CreatedAt,
				// Delta cleared — this is now a full snapshot.
			}

			if err := c.writer.Update(ctx, compactionRecord); err != nil {
				return compacted, fmt.Errorf("%w: compaction write v%d: %v", domain.ErrStorage, r.Version, err)
			}

			compacted++
			chainLen = 0 // reset — this is now a snapshot
		}
	}

	return compacted, nil
}

// extractEntityIDFromReconstructed extracts the entity ID from a
// reconstructed entity using the registry config.
func extractEntityIDFromReconstructed(entity any, ecfg registry.EntityConfig) (string, error) {
	v := reflect.ValueOf(entity)
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", fmt.Errorf("%w: reconstructed entity must be a struct", domain.ErrConfiguration)
	}

	idField := v.FieldByName(ecfg.IDField)
	if !idField.IsValid() {
		return "", fmt.Errorf("%w: ID field %q not found", domain.ErrConfiguration, ecfg.IDField)
	}

	return fmt.Sprintf("%v", idField.Interface()), nil
}
