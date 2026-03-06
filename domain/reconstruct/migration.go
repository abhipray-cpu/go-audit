package reconstruct

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// MigrationRegistry manages schema evolution migration functions.
// Migrations are registered as (entityType, fromVersion → toVersion) pairs
// and applied lazily on read.
type MigrationRegistry struct {
	mu         sync.RWMutex
	migrations map[string]map[int]migration // entityType → fromVersion → migration
}

type migration struct {
	From int
	To   int
	Fn   port.MigrationFunc
}

// NewMigrationRegistry creates a new empty MigrationRegistry.
func NewMigrationRegistry() *MigrationRegistry {
	return &MigrationRegistry{
		migrations: make(map[string]map[int]migration),
	}
}

// Register adds a migration function for the given entity type.
// fromVersion and toVersion must be consecutive (toVersion == fromVersion+1).
// Returns [domain.ErrMigration] if there is a gap or conflict.
func (r *MigrationRegistry) Register(entityType string, fromVersion, toVersion int, fn port.MigrationFunc) error {
	if fn == nil {
		return fmt.Errorf("%w: migration function must not be nil", domain.ErrMigration)
	}
	if toVersion != fromVersion+1 {
		return fmt.Errorf("%w: migration gap: v%d→v%d (must be consecutive, expected v%d→v%d)",
			domain.ErrMigration, fromVersion, toVersion, fromVersion, fromVersion+1)
	}
	if fromVersion < 1 {
		return fmt.Errorf("%w: fromVersion must be ≥ 1, got %d", domain.ErrMigration, fromVersion)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	byEntity, ok := r.migrations[entityType]
	if !ok {
		byEntity = make(map[int]migration)
		r.migrations[entityType] = byEntity
	}

	if _, exists := byEntity[fromVersion]; exists {
		return fmt.Errorf("%w: migration for %q v%d→v%d already registered",
			domain.ErrMigration, entityType, fromVersion, toVersion)
	}

	byEntity[fromVersion] = migration{From: fromVersion, To: toVersion, Fn: fn}
	return nil
}

// Migrate applies all registered migrations to transform data from
// fromSchemaVersion to toSchemaVersion. Each step is applied in order.
// Returns the transformed data and the final schema version reached.
func (r *MigrationRegistry) Migrate(ctx context.Context, entityType string, data []byte, fromSchemaVersion, toSchemaVersion int) ([]byte, int, error) {
	if fromSchemaVersion >= toSchemaVersion {
		return data, fromSchemaVersion, nil
	}

	r.mu.RLock()
	byEntity := r.migrations[entityType]
	r.mu.RUnlock()

	if byEntity == nil {
		return nil, fromSchemaVersion, fmt.Errorf(
			"%w: no migrations registered for %q", domain.ErrMigration, entityType)
	}

	current := data
	currentVersion := fromSchemaVersion

	for currentVersion < toSchemaVersion {
		m, ok := byEntity[currentVersion]
		if !ok {
			return nil, currentVersion, fmt.Errorf(
				"%w: missing migration for %q v%d→v%d",
				domain.ErrMigration, entityType, currentVersion, currentVersion+1)
		}

		migrated, err := m.Fn(ctx, current)
		if err != nil {
			return nil, currentVersion, fmt.Errorf(
				"%w: migration %q v%d→v%d failed: %v",
				domain.ErrMigration, entityType, m.From, m.To, err)
		}

		current = migrated
		currentVersion = m.To
	}

	return current, currentVersion, nil
}

// LatestSchemaVersion returns the highest schema version reachable
// via registered migrations for the given entity type.
// Returns 1 if no migrations are registered.
func (r *MigrationRegistry) LatestSchemaVersion(entityType string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	byEntity := r.migrations[entityType]
	if len(byEntity) == 0 {
		return 1
	}

	// Collect all "To" versions and return the max.
	versions := make([]int, 0, len(byEntity))
	for _, m := range byEntity {
		versions = append(versions, m.To)
	}
	sort.Ints(versions)
	return versions[len(versions)-1]
}

// HasMigrations returns true if any migrations are registered for the entity type.
func (r *MigrationRegistry) HasMigrations(entityType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.migrations[entityType]) > 0
}
