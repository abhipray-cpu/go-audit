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

// Engine reconstructs entity state from the nearest snapshot plus a chain
// of deltas. It works with any combination of Full, Delta, and Hybrid
// strategies.
type Engine struct {
	reader     port.VersionReaderPort
	serializer port.SerializerPort
	differ     port.DifferPort
	registry   *registry.Registry
}

// NewEngine creates a reconstruction Engine.
func NewEngine(
	reader port.VersionReaderPort,
	serializer port.SerializerPort,
	differ port.DifferPort,
	reg *registry.Registry,
) *Engine {
	return &Engine{
		reader:     reader,
		serializer: serializer,
		differ:     differ,
		registry:   reg,
	}
}

// Reconstruct rebuilds the entity state at the specified version by finding
// the nearest full snapshot and applying any subsequent deltas.
//
// Returns the reconstructed entity as an interface{} (pointer to struct),
// or an error if the version does not exist or reconstruction fails.
//
// This works correctly with Full, Delta, and Hybrid strategies:
//   - If the target version IS a full snapshot, it simply deserializes it.
//   - If the target version is a delta, it finds the nearest earlier snapshot,
//     deserializes it, and applies each delta in sequence up to the target.
func (e *Engine) Reconstruct(ctx context.Context, entityType, entityID string, targetVersion int64) (any, error) {
	// 1. Look up entity config from registry for type information.
	ecfg, err := e.registry.Get(entityType)
	if err != nil {
		return nil, fmt.Errorf("%w: unknown entity type %q", domain.ErrConfiguration, entityType)
	}

	// 2. Fetch the target version record.
	targetRecord, err := e.reader.GetByVersion(ctx, entityType, entityID, targetVersion)
	if err != nil {
		return nil, fmt.Errorf("%w: version %d not found", domain.ErrNotFound, targetVersion)
	}

	// 3. If the target is a full snapshot, just deserialize.
	if targetRecord.Strategy == domain.StrategyFull {
		entity := reflect.New(ecfg.ReflectType).Interface()
		if err := e.serializer.Unmarshal(ctx, targetRecord.Data, entity); err != nil {
			return nil, fmt.Errorf("%w: unmarshal target snapshot: %v", domain.ErrSerialization, err)
		}
		return entity, nil
	}

	// 4. Walk backwards to find the nearest full snapshot.
	//    Collect all records from snapshot to target (inclusive).
	records, err := e.reader.ListVersions(ctx, entityType, entityID, port.WithOrderAsc())
	if err != nil {
		return nil, fmt.Errorf("%w: list versions: %v", domain.ErrStorage, err)
	}

	// Sort by version ascending (should already be, but ensure).
	sort.Slice(records, func(i, j int) bool {
		return records[i].Version < records[j].Version
	})

	// Find the nearest snapshot at or before targetVersion.
	snapshotIdx := -1
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		if r.Version > targetVersion {
			continue
		}
		if r.Strategy == domain.StrategyFull {
			snapshotIdx = i
			break
		}
	}

	if snapshotIdx < 0 {
		return nil, fmt.Errorf("%w: no snapshot found before version %d", domain.ErrNotFound, targetVersion)
	}

	// 5. Deserialize the snapshot.
	snapshot := records[snapshotIdx]
	entity := reflect.New(ecfg.ReflectType).Interface()
	if err := e.serializer.Unmarshal(ctx, snapshot.Data, entity); err != nil {
		return nil, fmt.Errorf("%w: unmarshal snapshot v%d: %v", domain.ErrSerialization, snapshot.Version, err)
	}

	// 6. Apply each delta from snapshot+1 to target (inclusive).
	for i := snapshotIdx + 1; i < len(records); i++ {
		r := records[i]
		if r.Version > targetVersion {
			break
		}

		if r.Strategy == domain.StrategyFull {
			// If we encounter another snapshot in the chain, just use it.
			entity = reflect.New(ecfg.ReflectType).Interface()
			if err := e.serializer.Unmarshal(ctx, r.Data, entity); err != nil {
				return nil, fmt.Errorf("%w: unmarshal snapshot v%d: %v", domain.ErrSerialization, r.Version, err)
			}
			continue
		}

		// Apply delta.
		if r.Delta == nil || r.Delta.IsEmpty() {
			// No-op delta — entity unchanged.
			continue
		}

		entity, err = e.differ.Apply(ctx, entity, *r.Delta)
		if err != nil {
			return nil, fmt.Errorf("%w: apply delta v%d: %v", domain.ErrDiff, r.Version, err)
		}
	}

	return entity, nil
}
