package gcs

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time interface check.
var _ port.ColdStorePort = (*Store)(nil)

// GCSAPI abstracts the GCS operations needed by [Store].
// This matches the subset of the GCS client library used by this adapter.
type GCSAPI interface {
	// Write stores data at the given bucket/key.
	Write(ctx context.Context, bucket, key string, data io.Reader) error

	// Read retrieves data from the given bucket/key.
	Read(ctx context.Context, bucket, key string) (io.ReadCloser, error)
}

// Compressor compresses and decompresses data for cold storage.
// It is identical to [port.Compressor] — any implementation satisfying
// one interface satisfies both.
type Compressor = port.Compressor

// Config holds configuration for the GCS cold store.
type Config struct {
	// Bucket is the GCS bucket name.
	Bucket string

	// Client is the GCS API implementation.
	Client GCSAPI

	// Compressor is the compression implementation. If nil, data is
	// stored uncompressed.
	Compressor Compressor
}

// Store implements [port.ColdStorePort] for Google Cloud Storage.
type Store struct {
	bucket     string
	client     GCSAPI
	compressor Compressor
}

// New creates a new GCS cold store with the given configuration.
func New(cfg Config) *Store {
	c := cfg.Compressor
	if c == nil {
		c = &noopCompressor{}
	}
	return &Store{
		bucket:     cfg.Bucket,
		client:     cfg.Client,
		compressor: c,
	}
}

// Put stores data in GCS with optional compression.
func (s *Store) Put(ctx context.Context, key string, data []byte) error {
	compressed, err := s.compressor.Compress(data)
	if err != nil {
		return fmt.Errorf("gcs: compress: %w", err)
	}

	if err := s.client.Write(ctx, s.bucket, key, bytes.NewReader(compressed)); err != nil {
		return fmt.Errorf("gcs: write object %q: %w", key, err)
	}
	return nil
}

// Get retrieves data from GCS and decompresses it.
func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	rc, err := s.client.Read(ctx, s.bucket, key)
	if err != nil {
		return nil, fmt.Errorf("gcs: read object %q: %w", key, err)
	}
	defer rc.Close()

	compressed, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("gcs: read body %q: %w", key, err)
	}

	data, err := s.compressor.Decompress(compressed)
	if err != nil {
		return nil, fmt.Errorf("gcs: decompress %q: %w", key, err)
	}
	return data, nil
}

// ---------- noop compressor ----------

type noopCompressor struct{}

func (n *noopCompressor) Compress(data []byte) ([]byte, error)   { return data, nil }
func (n *noopCompressor) Decompress(data []byte) ([]byte, error) { return data, nil }
