package registry

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/abhipray-cpu/go-audit/domain"
)

// FieldConfig describes a single auditable field on a registered entity.
type FieldConfig struct {
	// Name is the Go struct field name.
	Name string

	// Index is the struct field index (for reflect.Value.Field).
	Index int

	// Type is the reflect.Type of the field.
	Type reflect.Type

	// ID is true when the field is tagged version:"id".
	ID bool

	// Tracked is true when the field is tagged version:"tracked".
	Tracked bool

	// Ignore is true when the field is tagged version:"ignore".
	Ignore bool

	// Normalized is true when the field is tagged version:"normalized".
	Normalized bool

	// Redactable is true when the field is tagged version:"redactable".
	Redactable bool
}

// EntityConfig holds the parsed configuration for a registered entity type.
type EntityConfig struct {
	// TypeName is the lowercased struct name (e.g. "user").
	TypeName string

	// ReflectType is the original reflect.Type of the entity struct.
	ReflectType reflect.Type

	// IDField is the name of the field tagged version:"id".
	IDField string

	// Fields is the ordered list of exported field configurations.
	Fields []FieldConfig

	// HasTracked is true if any field has the "tracked" tag.
	HasTracked bool
}

// Registry manages entity type registration and lookup. It is safe for
// concurrent use.
type Registry struct {
	mu         sync.RWMutex
	entities   map[string]*EntityConfig
	guardrails *GuardrailConfig
}

// New returns a new empty Registry. Options (e.g. [WithGuardrails]) can be
// provided to customize behavior.
func New(opts ...Option) *Registry {
	r := &Registry{
		entities: make(map[string]*EntityConfig),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Register inspects the given entity (struct or pointer-to-struct) via
// reflection, parses its version:"..." tags, and stores the resulting
// [EntityConfig] for later lookup.
//
// The entity must have exactly one field tagged version:"id". The type name
// is derived from the struct name lowercased (e.g. *User → "user").
//
// Returns [domain.ErrConfiguration] if:
//   - the entity is not a struct or pointer-to-struct
//   - no field is tagged version:"id"
//   - the entity type is already registered
func (r *Registry) Register(entity any) error {
	t := reflect.TypeOf(entity)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("%w: entity must be a struct, got %s", domain.ErrConfiguration, t.Kind())
	}

	typeName := strings.ToLower(t.Name())
	if typeName == "" {
		return fmt.Errorf("%w: anonymous structs cannot be registered", domain.ErrConfiguration)
	}

	r.mu.RLock()
	_, exists := r.entities[typeName]
	r.mu.RUnlock()
	if exists {
		return fmt.Errorf("%w: entity type %q is already registered", domain.ErrConfiguration, typeName)
	}

	cfg, err := buildEntityConfig(t, typeName)
	if err != nil {
		return err
	}

	// Run guardrails before storing the config.
	if err := r.checkGuardrails(entity, typeName); err != nil {
		return err
	}

	r.mu.Lock()
	// Double-check after write lock.
	if _, exists := r.entities[typeName]; exists {
		r.mu.Unlock()
		return fmt.Errorf("%w: entity type %q is already registered", domain.ErrConfiguration, typeName)
	}
	r.entities[typeName] = cfg
	r.mu.Unlock()

	return nil
}

// Get returns the [EntityConfig] for the given entity type name.
// Returns [domain.ErrConfiguration] if the type is not registered.
func (r *Registry) Get(entityType string) (EntityConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cfg, ok := r.entities[entityType]
	if !ok {
		return EntityConfig{}, fmt.Errorf(
			"%w: entity type %q is not registered",
			domain.ErrConfiguration, entityType,
		)
	}
	return *cfg, nil
}

// RegisteredTypes returns the names of all registered entity types.
func (r *Registry) RegisteredTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.entities))
	for name := range r.entities {
		types = append(types, name)
	}
	return types
}

// buildEntityConfig inspects the struct type and returns a fully populated
// EntityConfig, or an error if validation fails.
func buildEntityConfig(t reflect.Type, typeName string) (*EntityConfig, error) {
	cfg := &EntityConfig{
		TypeName:    typeName,
		ReflectType: t,
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		fc := parseFieldConfig(f, i)
		cfg.Fields = append(cfg.Fields, fc)

		if fc.ID {
			if cfg.IDField != "" {
				return nil, fmt.Errorf(
					"%w: entity %q has multiple version:\"id\" fields: %q and %q",
					domain.ErrConfiguration, typeName, cfg.IDField, fc.Name,
				)
			}
			cfg.IDField = fc.Name
		}

		if fc.Tracked {
			cfg.HasTracked = true
		}
	}

	if cfg.IDField == "" {
		return nil, fmt.Errorf(
			"%w: entity %q has no field tagged version:\"id\"",
			domain.ErrConfiguration, typeName,
		)
	}

	return cfg, nil
}
