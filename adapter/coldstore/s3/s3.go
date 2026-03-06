package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time interface check.
var _ port.ColdStorePort = (*Store)(nil)

// S3API abstracts the S3 operations needed by [Store].
// This matches the subset of the AWS SDK v2 s3.Client used by this adapter.
type S3API interface {
	PutObject(ctx context.Context, input *PutObjectInput) error
	GetObject(ctx context.Context, input *GetObjectInput) (*GetObjectOutput, error)
}

// PutObjectInput is the input for [S3API.PutObject].
type PutObjectInput struct {
	Bucket          string
	Key             string
	Body            io.Reader
	ContentEncoding string
}

// GetObjectInput is the input for [S3API.GetObject].
type GetObjectInput struct {
	Bucket string
	Key    string
}

// GetObjectOutput is the output from [S3API.GetObject].
type GetObjectOutput struct {
	Body io.ReadCloser
}

// Compressor compresses and decompresses data for cold storage.
// It extends the base [port.Compressor] contract with a
// [Compressor.ContentEncoding] method so the correct Content-Encoding
// header is set on stored S3 objects.
type Compressor interface {
	port.Compressor

	// ContentEncoding returns the IANA content coding name for the
	// compression algorithm (e.g., "zstd", "gzip", "identity").
	ContentEncoding() string
}

// Config holds configuration for the S3 cold store.
type Config struct {
	// Bucket is the S3 bucket name.
	Bucket string

	// Client is the S3 API implementation.
	Client S3API

	// Compressor is the compression implementation. If nil, a noop
	// compressor is used (data stored uncompressed).
	Compressor Compressor
}

// Store implements [port.ColdStorePort] for Amazon S3 and S3-compatible
// object stores.
type Store struct {
	bucket     string
	client     S3API
	compressor Compressor
}

// New creates a new S3 cold store with the given configuration.
func New(cfg Config) *Store {
	c := cfg.Compressor
	if c == nil {
		c = &PassthroughCompressor{}
	}
	return &Store{
		bucket:     cfg.Bucket,
		client:     cfg.Client,
		compressor: c,
	}
}

// Put stores data in S3 with optional compression.
// The key is typically "{entity_type}/{entity_id}/{version}".
func (s *Store) Put(ctx context.Context, key string, data []byte) error {
	compressed, err := s.compressor.Compress(data)
	if err != nil {
		return fmt.Errorf("s3: compress: %w", err)
	}

	err = s.client.PutObject(ctx, &PutObjectInput{
		Bucket:          s.bucket,
		Key:             key,
		Body:            bytes.NewReader(compressed),
		ContentEncoding: s.compressor.ContentEncoding(),
	})
	if err != nil {
		return fmt.Errorf("s3: put object %q: %w", key, err)
	}
	return nil
}

// Get retrieves data from S3 and decompresses it.
func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	out, err := s.client.GetObject(ctx, &GetObjectInput{
		Bucket: s.bucket,
		Key:    key,
	})
	if err != nil {
		return nil, fmt.Errorf("s3: get object %q: %w", key, err)
	}
	defer out.Body.Close()

	compressed, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("s3: read body %q: %w", key, err)
	}

	data, err := s.compressor.Decompress(compressed)
	if err != nil {
		return nil, fmt.Errorf("s3: decompress %q: %w", key, err)
	}
	return data, nil
}

// ---------- passthrough compressor ----------

// PassthroughCompressor is a reference [Compressor] implementation that
// copies data without actual compression. It reports a content encoding
// of "identity".
//
// For production use with real zstd compression, import
// github.com/klauspost/compress/zstd and wrap it to implement [Compressor]
// with ContentEncoding() returning "zstd".
type PassthroughCompressor struct{}

// Compress returns a copy of data without compression.
func (p *PassthroughCompressor) Compress(data []byte) ([]byte, error) {
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

// Decompress returns a copy of data without decompression.
func (p *PassthroughCompressor) Decompress(data []byte) ([]byte, error) {
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

// ContentEncoding returns "identity" (no encoding).
func (p *PassthroughCompressor) ContentEncoding() string { return "identity" }
