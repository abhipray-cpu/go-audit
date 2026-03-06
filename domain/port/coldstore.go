package port

import (
	"context"
)

// ColdStorePort provides access to compressed cold storage (e.g., S3, GCS)
// for archived version records.
type ColdStorePort interface {
	// Put stores a serialized version record in cold storage.
	// The key is typically "{entity_type}/{entity_id}/{version}".
	Put(ctx context.Context, key string, data []byte) error

	// Get retrieves a serialized version record from cold storage.
	Get(ctx context.Context, key string) ([]byte, error)
}

// Compressor compresses and decompresses data for cold storage adapters.
// Both the S3 and GCS cold store adapters accept this interface so a
// single compressor implementation can be shared between stores.
type Compressor interface {
	Compress(data []byte) ([]byte, error)
	Decompress(data []byte) ([]byte, error)
}
