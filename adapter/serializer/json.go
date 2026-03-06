package serializer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time interface check.
var _ port.SerializerPort = (*JSONSerializer)(nil)

// JSONSerializer is the default SerializerPort backed by encoding/json.
type JSONSerializer struct{}

// NewJSON returns a ready-to-use JSONSerializer.
func NewJSON() *JSONSerializer { return &JSONSerializer{} }

// Marshal serializes v into JSON bytes.
// Returns an error if v is nil.
func (s *JSONSerializer) Marshal(_ context.Context, v any) ([]byte, error) {
	if v == nil {
		return nil, fmt.Errorf("serializer: cannot marshal nil value")
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("serializer: marshal failed: %w", err)
	}
	return data, nil
}

// Unmarshal deserializes JSON data into target.
// Returns an error if data is nil/empty or target is nil.
func (s *JSONSerializer) Unmarshal(_ context.Context, data []byte, target any) error {
	if target == nil {
		return fmt.Errorf("serializer: target must not be nil")
	}
	if len(data) == 0 {
		return fmt.Errorf("serializer: data must not be empty")
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("serializer: unmarshal failed: %w", err)
	}
	return nil
}

// ContentType returns the MIME type for JSON.
func (s *JSONSerializer) ContentType() string { return "application/json" }
