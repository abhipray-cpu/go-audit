package registry

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// DefaultMaxEntityBytes is the default maximum serialized size (5 MB).
const DefaultMaxEntityBytes int64 = 5 * 1024 * 1024

// GuardrailConfig controls registration-time guardrail behavior.
type GuardrailConfig struct {
	// StrictRegistration turns warnings into errors. When true, any
	// guardrail warning (e.g. unignored []byte field) causes Register
	// to return an error instead of just logging.
	StrictRegistration bool

	// MaxEntityBytes is the maximum allowed JSON-serialized size of an
	// entity. Entities exceeding this limit are rejected at registration
	// time. Defaults to [DefaultMaxEntityBytes] (5 MB).
	MaxEntityBytes int64

	// Logger receives guardrail warnings. If nil, warnings are silently
	// discarded (useful in tests without a logger).
	Logger port.LoggerPort
}

// Option configures the [Registry].
type Option func(*Registry)

// WithGuardrails enables registration-time guardrails.
func WithGuardrails(cfg GuardrailConfig) Option {
	return func(r *Registry) {
		if cfg.MaxEntityBytes == 0 {
			cfg.MaxEntityBytes = DefaultMaxEntityBytes
		}
		r.guardrails = &cfg
	}
}

// checkGuardrails runs registration-time analysis on an entity.
// It returns an error if a hard constraint is violated (e.g. oversized entity)
// or if StrictRegistration is true and a warning condition is found.
//
// Warning conditions:
//   - []byte field without version:"ignore" tag
//
// Hard constraints:
//   - entity serialized size > MaxEntityBytes
func (r *Registry) checkGuardrails(entity any, typeName string) error {
	if r.guardrails == nil {
		return nil
	}

	g := r.guardrails

	// Check for []byte fields without ignore tag.
	t := reflect.TypeOf(entity)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	byteSliceType := reflect.TypeOf([]byte(nil))
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Type == byteSliceType {
			fc := parseFieldConfig(f, i)
			if !fc.Ignore {
				msg := fmt.Sprintf(
					"entity %q field %q is []byte without version:\"ignore\" — consider ignoring it to avoid large diffs",
					typeName, f.Name,
				)
				if g.StrictRegistration {
					return fmt.Errorf("%w: %s", domain.ErrConfiguration, msg)
				}
				if g.Logger != nil {
					g.Logger.Warn(msg, "entity", typeName, "field", f.Name)
				}
			}
		}
	}

	// Check serialized size.
	data, err := json.Marshal(entity)
	if err != nil {
		return fmt.Errorf(
			"%w: failed to estimate entity %q size: %s",
			domain.ErrConfiguration, typeName, err,
		)
	}
	size := int64(len(data))
	if size > g.MaxEntityBytes {
		return &domain.EntityTooLargeError{
			EntityType: typeName,
			Size:       size,
			Limit:      g.MaxEntityBytes,
		}
	}

	return nil
}
