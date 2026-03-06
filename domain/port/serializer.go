package port

import (
	"context"
)

// SerializerPort marshals and unmarshals entity data for storage.
type SerializerPort interface {
	// Marshal serializes an entity into bytes.
	Marshal(ctx context.Context, v any) ([]byte, error)

	// Unmarshal deserializes bytes into the target entity.
	Unmarshal(ctx context.Context, data []byte, target any) error

	// ContentType returns the MIME type of the serialization format
	// (e.g., "application/json", "application/msgpack").
	ContentType() string
}
