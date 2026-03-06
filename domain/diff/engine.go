package diff

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
)

// timeType is cached so we can special-case time.Time (compare as leaf, not recurse).
var timeType = reflect.TypeOf(time.Time{})

// tagFlags holds the parsed values from a `version:"..."` struct tag.
// Multiple values are comma-separated, e.g. `version:"tracked,redactable"`.
type tagFlags struct {
	id         bool // version:"id"         — entity identifier, never appears in delta
	tracked    bool // version:"tracked"    — explicitly opt-in for diffing
	ignore     bool // version:"ignore"     — explicitly excluded from diffing
	redactable bool // version:"redactable" — informational flag for later use
	normalized bool // version:"normalized" — sort slices before comparison
}

// parseVersionTag parses the `version` struct tag value into tagFlags.
func parseVersionTag(tag string) tagFlags {
	var f tagFlags
	for _, part := range strings.Split(tag, ",") {
		switch strings.TrimSpace(part) {
		case "id":
			f.id = true
		case "tracked":
			f.tracked = true
		case "ignore":
			f.ignore = true
		case "redactable":
			f.redactable = true
		case "normalized":
			f.normalized = true
		}
	}
	return f
}

// fieldMeta holds pre-computed metadata for a single exported struct field.
type fieldMeta struct {
	index      int
	name       string
	comparable bool // true when == can replace reflect.DeepEqual
	embedded   bool // true for anonymous (promoted) struct fields
	tags       tagFlags
}

// typeMeta holds the cached field metadata for a struct type.
type typeMeta struct {
	fields     []fieldMeta
	hasTracked bool // true if any field has version:"tracked"
}

// defaultMaxCachedTypes is the maximum number of distinct struct types the
// Engine will cache metadata for. This bounds memory growth to O(maxTypes)
// regardless of input. A typical service audits 5–50 entity types; 128
// provides generous headroom while preventing runaway growth from misuse.
const defaultMaxCachedTypes = 128

// Engine performs field-by-field structural diffing of Go structs using reflection.
// It compares exported fields of flat structs and produces a [domain.Delta]
// describing which fields changed along with their JSON-encoded old and new values.
//
// Field metadata is cached per struct type so that repeated diffs of the same
// entity type avoid redundant reflection. The cache is bounded by maxTypes
// (default 128) to prevent unbounded memory growth.
type Engine struct {
	mu       sync.RWMutex
	cache    map[reflect.Type]*typeMeta
	maxTypes int
}

// Option configures the diff Engine.
type Option func(*Engine)

// WithMaxCachedTypes sets the upper bound on cached struct type metadata.
// When the limit is reached, new (uncached) types return ErrDiff instead
// of silently growing memory. Set to 0 for unlimited (not recommended).
func WithMaxCachedTypes(n int) Option {
	return func(e *Engine) {
		e.maxTypes = n
	}
}

// New returns a new diff Engine with an empty type cache.
func New(opts ...Option) *Engine {
	e := &Engine{
		cache:    make(map[reflect.Type]*typeMeta),
		maxTypes: defaultMaxCachedTypes,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// CachedTypes returns the number of struct types currently in the metadata
// cache. Useful for monitoring/metrics.
func (e *Engine) CachedTypes() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.cache)
}

// Diff compares prev and curr (which must be the same struct type or pointers
// to the same struct type) and returns a [domain.Delta] listing every
// exported field whose value changed. OldValue and NewValue are JSON-encoded.
//
// Nested structs produce dotted paths (e.g. "Address.City"). Embedded
// (anonymous) struct fields are promoted — their children appear as
// top-level paths unless the embedded field itself has a version tag.
//
// For fields whose types are directly comparable (string, int, bool, etc.),
// a fast == check is used instead of reflect.DeepEqual.
//
// Returns [domain.ErrDiff] if the inputs are not comparable struct types.
func (e *Engine) Diff(_ context.Context, prev, curr any) (domain.Delta, error) {
	prevVal, err := derefStruct(prev)
	if err != nil {
		return domain.Delta{}, fmt.Errorf("%w: prev: %s", domain.ErrDiff, err)
	}
	currVal, err := derefStruct(curr)
	if err != nil {
		return domain.Delta{}, fmt.Errorf("%w: curr: %s", domain.ErrDiff, err)
	}

	if prevVal.Type() != currVal.Type() {
		return domain.Delta{}, fmt.Errorf(
			"%w: type mismatch: prev is %s, curr is %s",
			domain.ErrDiff, prevVal.Type(), currVal.Type(),
		)
	}

	meta, err := e.getOrBuildMeta(prevVal.Type())
	if err != nil {
		return domain.Delta{}, err
	}

	// Pre-allocate with a small capacity; most diffs change a few fields.
	changes := make([]domain.FieldChange, 0, 4)

	changes, err = e.diffFields(meta, prevVal, currVal, "", changes)
	if err != nil {
		return domain.Delta{}, err
	}

	return domain.Delta{Changes: changes}, nil
}

// diffFields compares the fields described by meta between prevVal and currVal,
// appending any changes to the changes slice. prefix is prepended to field
// paths with a dot separator (empty for the root struct).
func (e *Engine) diffFields(
	meta *typeMeta,
	prevVal, currVal reflect.Value,
	prefix string,
	changes []domain.FieldChange,
) ([]domain.FieldChange, error) {

	for _, fm := range meta.fields {
		// Skip fields tagged as id or ignore — they never appear in the delta.
		if fm.tags.id || fm.tags.ignore {
			continue
		}

		// If any field is tagged "tracked", only tracked fields are compared.
		// Embedded (anonymous) struct fields are always traversed since they
		// are structural containers whose promoted children may be tracked.
		if meta.hasTracked && !fm.tags.tracked && !fm.embedded {
			continue
		}

		oldField := prevVal.Field(fm.index)
		newField := currVal.Field(fm.index)

		path := fm.name
		if prefix != "" {
			path = prefix + "." + fm.name
		}

		// For slice fields with the "normalized" tag, sort copies before comparing
		// so that element ordering does not cause spurious diffs.
		fieldType := oldField.Type()
		if fm.tags.normalized && fieldType.Kind() == reflect.Slice {
			oldField = sortedSliceCopy(oldField)
			newField = sortedSliceCopy(newField)
		}

		// Unsupported types: chan, func, unsafe.Pointer → fallback.
		switch fieldType.Kind() {
		case reflect.Chan, reflect.Func, reflect.UnsafePointer:
			return nil, fmt.Errorf(
				"%w: unsupported field type %s at %s",
				domain.ErrDiffFallback, fieldType.Kind(), path,
			)
		}

		// For struct fields (non-embedded): recurse with dotted path.
		// For embedded (anonymous) fields: promote children to current prefix.
		if fieldType.Kind() == reflect.Struct && fieldType != timeType {
			childPrefix := path
			if fm.embedded {
				// Promoted — keep the current prefix.
				childPrefix = prefix
			}
			childMeta, err := e.getOrBuildMeta(fieldType)
			if err != nil {
				return nil, err
			}
			// When an embedded struct has no tracked tags of its own but
			// the parent does, propagate hasTracked so the filter inside
			// the child still skips untagged fields.
			effectiveMeta := childMeta
			if fm.embedded && meta.hasTracked && !childMeta.hasTracked {
				cp := *childMeta
				cp.hasTracked = true
				effectiveMeta = &cp
			}
			var err2 error
			changes, err2 = e.diffFields(effectiveMeta, oldField, newField, childPrefix, changes)
			if err2 != nil {
				return nil, err2
			}
			continue
		}

		// For map fields with string keys: compare per-key with sorted iteration.
		if fieldType.Kind() == reflect.Map && fieldType.Key().Kind() == reflect.String {
			var err2 error
			changes, err2 = e.diffMap(oldField, newField, path, changes)
			if err2 != nil {
				return nil, err2
			}
			continue
		}

		// For pointer fields: handle nil transitions and deref for comparison.
		if fieldType.Kind() == reflect.Pointer {
			oldNil := !oldField.IsValid() || oldField.IsNil()
			newNil := !newField.IsValid() || newField.IsNil()

			if oldNil && newNil {
				continue
			}

			if oldNil != newNil {
				// nil→value or value→nil transition.
				var oldJSON, newJSON json.RawMessage
				if !oldNil {
					b, err := json.Marshal(oldField.Interface())
					if err != nil {
						return nil, fmt.Errorf("%w: failed to marshal old pointer value at %s: %s", domain.ErrDiff, path, err)
					}
					oldJSON = b
				}
				if !newNil {
					b, err := json.Marshal(newField.Interface())
					if err != nil {
						return nil, fmt.Errorf("%w: failed to marshal new pointer value at %s: %s", domain.ErrDiff, path, err)
					}
					newJSON = b
				}
				changes = append(changes, domain.FieldChange{Path: path, OldValue: oldJSON, NewValue: newJSON})
				continue
			}

			// Both non-nil: deref and compare the pointed-to values.
			oldField = oldField.Elem()
			newField = newField.Elem()
			fieldType = oldField.Type()

			// After dereferencing, the pointed-to value might be a struct — recurse.
			if fieldType.Kind() == reflect.Struct && fieldType != timeType {
				childMeta, err := e.getOrBuildMeta(fieldType)
				if err != nil {
					return nil, err
				}
				var err2 error
				changes, err2 = e.diffFields(childMeta, oldField, newField, path, changes)
				if err2 != nil {
					return nil, err2
				}
				continue
			}
		}

		// Special case: time.Time uses .Equal() for timezone-independent comparison.
		if fieldType == timeType {
			oldTime := oldField.Interface().(time.Time)
			newTime := newField.Interface().(time.Time)
			if oldTime.Equal(newTime) {
				continue
			}
		} else {
			// Leaf comparison.
			if fm.comparable && fieldType.Comparable() {
				if oldField.Interface() == newField.Interface() {
					continue
				}
			} else {
				if reflect.DeepEqual(oldField.Interface(), newField.Interface()) {
					continue
				}
			}
		}

		oldJSON, err := json.Marshal(oldField.Interface())
		if err != nil {
			return nil, fmt.Errorf(
				"%w: failed to marshal old value of field %s: %s",
				domain.ErrDiff, path, err,
			)
		}

		newJSON, err := json.Marshal(newField.Interface())
		if err != nil {
			return nil, fmt.Errorf(
				"%w: failed to marshal new value of field %s: %s",
				domain.ErrDiff, path, err,
			)
		}

		changes = append(changes, domain.FieldChange{
			Path:     path,
			OldValue: oldJSON,
			NewValue: newJSON,
		})
	}

	return changes, nil
}

// Apply reconstructs an entity by applying a delta to a previous state.
// prev must be a pointer to a struct. The returned value is a new copy of
// the struct with the delta's changes applied.
func (e *Engine) Apply(_ context.Context, prev any, delta domain.Delta) (any, error) {
	if delta.IsEmpty() {
		return prev, nil
	}

	prevVal := reflect.ValueOf(prev)
	if prevVal.Kind() == reflect.Pointer {
		if prevVal.IsNil() {
			return nil, fmt.Errorf("%w: prev is nil pointer", domain.ErrDiff)
		}
		prevVal = prevVal.Elem()
	}
	if prevVal.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: prev must be a struct, got %s", domain.ErrDiff, prevVal.Kind())
	}

	// Create a mutable copy.
	cp := reflect.New(prevVal.Type()).Elem()
	cp.Set(prevVal)

	for _, change := range delta.Changes {
		if err := applyChange(cp, change); err != nil {
			return nil, fmt.Errorf("%w: %s", domain.ErrDiff, err)
		}
	}

	return cp.Interface(), nil
}

// applyChange sets a single field described by a FieldChange on the target value.
// It supports dotted paths (e.g. "Address.City") by walking into nested structs
// and map keys (e.g. "Meta.key").
func applyChange(target reflect.Value, change domain.FieldChange) error {
	parts := strings.Split(change.Path, ".")
	v := target

	for i, part := range parts {
		// Walk through pointers.
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}

		// Map navigation: remaining path segments are map keys.
		if v.Kind() == reflect.Map && v.Type().Key().Kind() == reflect.String {
			return applyMapPath(v, parts[i:], change.NewValue, change.OldValue)
		}

		if v.Kind() != reflect.Struct {
			return fmt.Errorf("expected struct at %q, got %s", strings.Join(parts[:i], "."), v.Kind())
		}

		// FieldByName finds both direct and promoted (embedded) fields.
		field := v.FieldByName(part)
		if !field.IsValid() {
			return fmt.Errorf("field %q not found", change.Path)
		}

		if i == len(parts)-1 {
			// Terminal field — set the value.
			return setFieldFromJSON(field, change.NewValue)
		}

		v = field
	}

	return nil
}

// applyMapPath handles applying a change within a map. pathParts contains
// the remaining path segments starting at the current map level.
func applyMapPath(m reflect.Value, pathParts []string, newVal, oldVal json.RawMessage) error {
	if len(pathParts) == 0 {
		return nil
	}

	key := reflect.ValueOf(pathParts[0])

	if len(pathParts) == 1 {
		// Terminal key.
		if newVal == nil {
			// Key removed.
			m.SetMapIndex(key, reflect.Value{})
			return nil
		}

		// Determine the type of value to unmarshal.
		existing := m.MapIndex(key)
		var valType reflect.Type
		if existing.IsValid() {
			if existing.Kind() == reflect.Interface {
				existing = existing.Elem()
			}
			valType = existing.Type()
		} else {
			// New key — use the map's value type.
			valType = m.Type().Elem()
		}

		ptr := reflect.New(valType)
		if err := json.Unmarshal(newVal, ptr.Interface()); err != nil {
			return fmt.Errorf("unmarshal map value: %w", err)
		}
		m.SetMapIndex(key, ptr.Elem())
		return nil
	}

	// Non-terminal: walk into nested map.
	child := m.MapIndex(key)
	if !child.IsValid() {
		// Create nested map.
		child = reflect.MakeMap(reflect.TypeOf(map[string]any{}))
		m.SetMapIndex(key, child)
	}
	if child.Kind() == reflect.Interface {
		child = child.Elem()
	}
	if child.Kind() == reflect.Map && child.Type().Key().Kind() == reflect.String {
		return applyMapPath(child, pathParts[1:], newVal, oldVal)
	}

	return fmt.Errorf("expected map at path segment %q, got %s", pathParts[0], child.Kind())
}

// setFieldFromJSON unmarshals a JSON-encoded value into a reflect.Value.
func setFieldFromJSON(field reflect.Value, raw json.RawMessage) error {
	if raw == nil {
		field.Set(reflect.Zero(field.Type()))
		return nil
	}

	// Create a pointer to a new value of the field's type, unmarshal into it,
	// then set the field.
	ptr := reflect.New(field.Type())
	if err := json.Unmarshal(raw, ptr.Interface()); err != nil {
		return fmt.Errorf("unmarshal into %s: %w", field.Type(), err)
	}
	field.Set(ptr.Elem())
	return nil
}

// sortedSliceCopy returns a reflect.Value containing a sorted copy of the
// input slice. Elements are sorted by their JSON representation, which
// gives deterministic ordering for any serializable type.
func sortedSliceCopy(v reflect.Value) reflect.Value {
	if v.IsNil() || v.Len() == 0 {
		return v
	}
	cp := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
	reflect.Copy(cp, v)

	sort.Sort(&sortableSlice{val: cp, swap: reflect.Swapper(cp.Interface())})
	return cp
}

// sortableSlice implements sort.Interface using a reflect.Value slice.
// Elements are compared by their JSON-serialized form for universal ordering.
type sortableSlice struct {
	val  reflect.Value
	swap func(i, j int)
}

func (s *sortableSlice) Len() int      { return s.val.Len() }
func (s *sortableSlice) Swap(i, j int) { s.swap(i, j) }
func (s *sortableSlice) Less(i, j int) bool {
	a, _ := json.Marshal(s.val.Index(i).Interface())
	b, _ := json.Marshal(s.val.Index(j).Interface())
	return string(a) < string(b)
}

// getOrBuildMeta returns cached field metadata for the given struct type,
// building and caching it on first access. This avoids repeated reflection
// overhead when the same entity type is diffed many times.
//
// Returns an error if the cache is full (maxTypes reached), which signals
// that too many distinct types are being diffed — likely a misuse.
func (e *Engine) getOrBuildMeta(t reflect.Type) (*typeMeta, error) {
	e.mu.RLock()
	m, ok := e.cache[t]
	e.mu.RUnlock()
	if ok {
		return m, nil
	}

	// Build metadata for all exported fields.
	var fields []fieldMeta
	hasTracked := false
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tags := parseVersionTag(f.Tag.Get("version"))
		if tags.tracked {
			hasTracked = true
		}
		fields = append(fields, fieldMeta{
			index:      i,
			name:       f.Name,
			comparable: f.Type.Comparable(),
			embedded:   f.Anonymous,
			tags:       tags,
		})
	}

	m = &typeMeta{fields: fields, hasTracked: hasTracked}

	e.mu.Lock()
	// Double-check after acquiring write lock.
	if existing, ok := e.cache[t]; ok {
		e.mu.Unlock()
		return existing, nil
	}
	if e.maxTypes > 0 && len(e.cache) >= e.maxTypes {
		e.mu.Unlock()
		return nil, fmt.Errorf(
			"%w: type cache full (%d types); register fewer entity types or increase WithMaxCachedTypes",
			domain.ErrDiff, e.maxTypes,
		)
	}
	e.cache[t] = m
	e.mu.Unlock()

	return m, nil
}

// diffMap compares two map[string]T values, iterating keys in sorted order.
// Added keys, removed keys, and changed values each produce a FieldChange.
// Nested maps (map[string]any) are recursed into with dotted paths.
func (e *Engine) diffMap(
	oldMap, newMap reflect.Value,
	prefix string,
	changes []domain.FieldChange,
) ([]domain.FieldChange, error) {
	// Collect all unique keys from both maps.
	keySet := make(map[string]struct{})

	if oldMap.IsValid() && !oldMap.IsNil() {
		for _, k := range oldMap.MapKeys() {
			keySet[k.String()] = struct{}{}
		}
	}
	if newMap.IsValid() && !newMap.IsNil() {
		for _, k := range newMap.MapKeys() {
			keySet[k.String()] = struct{}{}
		}
	}

	// Sort keys for deterministic output.
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		path := prefix + "." + key

		oldKey := reflect.ValueOf(key)
		oldExists := oldMap.IsValid() && !oldMap.IsNil() && oldMap.MapIndex(oldKey).IsValid()
		newExists := newMap.IsValid() && !newMap.IsNil() && newMap.MapIndex(oldKey).IsValid()

		switch {
		case oldExists && newExists:
			oldVal := oldMap.MapIndex(oldKey)
			newVal := newMap.MapIndex(oldKey)

			// Recurse into nested maps.
			if oldVal.Kind() == reflect.Interface {
				oldVal = oldVal.Elem()
			}
			if newVal.Kind() == reflect.Interface {
				newVal = newVal.Elem()
			}

			if oldVal.IsValid() && newVal.IsValid() &&
				oldVal.Kind() == reflect.Map && newVal.Kind() == reflect.Map &&
				oldVal.Type().Key().Kind() == reflect.String {
				var err error
				changes, err = e.diffMap(oldVal, newVal, path, changes)
				if err != nil {
					return nil, err
				}
				continue
			}

			// Leaf map value comparison.
			if reflect.DeepEqual(oldVal.Interface(), newVal.Interface()) {
				continue
			}

			oldJSON, err := json.Marshal(oldVal.Interface())
			if err != nil {
				return nil, fmt.Errorf("%w: failed to marshal old map value at %s: %s", domain.ErrDiff, path, err)
			}
			newJSON, err := json.Marshal(newVal.Interface())
			if err != nil {
				return nil, fmt.Errorf("%w: failed to marshal new map value at %s: %s", domain.ErrDiff, path, err)
			}
			changes = append(changes, domain.FieldChange{Path: path, OldValue: oldJSON, NewValue: newJSON})

		case oldExists && !newExists:
			// Key removed.
			oldVal := oldMap.MapIndex(oldKey)
			if oldVal.Kind() == reflect.Interface {
				oldVal = oldVal.Elem()
			}
			oldJSON, err := json.Marshal(oldVal.Interface())
			if err != nil {
				return nil, fmt.Errorf("%w: failed to marshal removed map value at %s: %s", domain.ErrDiff, path, err)
			}
			changes = append(changes, domain.FieldChange{Path: path, OldValue: oldJSON, NewValue: nil})

		case !oldExists && newExists:
			// Key added.
			newVal := newMap.MapIndex(oldKey)
			if newVal.Kind() == reflect.Interface {
				newVal = newVal.Elem()
			}
			newJSON, err := json.Marshal(newVal.Interface())
			if err != nil {
				return nil, fmt.Errorf("%w: failed to marshal added map value at %s: %s", domain.ErrDiff, path, err)
			}
			changes = append(changes, domain.FieldChange{Path: path, OldValue: nil, NewValue: newJSON})
		}
	}

	return changes, nil
}

// derefStruct returns the reflect.Value of v, dereferencing pointers until
// a struct is reached. Returns an error if v is not a struct (or pointer-to-struct).
func derefStruct(v any) (reflect.Value, error) {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return reflect.Value{}, fmt.Errorf("nil pointer")
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("expected struct, got %s", rv.Kind())
	}
	return rv, nil
}
