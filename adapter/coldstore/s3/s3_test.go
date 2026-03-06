package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
)

// ---------- in-memory S3 mock ----------

type memS3 struct {
	mu      sync.Mutex
	objects map[string][]byte // bucket/key → data
}

func newMemS3() *memS3 {
	return &memS3{objects: make(map[string][]byte)}
}

func (m *memS3) PutObject(_ context.Context, input *PutObjectInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := io.ReadAll(input.Body)
	if err != nil {
		return err
	}

	key := input.Bucket + "/" + input.Key
	m.objects[key] = data
	return nil
}

func (m *memS3) GetObject(_ context.Context, input *GetObjectInput) (*GetObjectOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := input.Bucket + "/" + input.Key
	data, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("NoSuchKey: %s", key)
	}

	return &GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(data)),
	}, nil
}

// ---------- test compressor ----------

type testCompressor struct {
	mu sync.Mutex
}

func (c *testCompressor) Compress(data []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Simple "compression": prepend a magic byte.
	out := make([]byte, 0, 1+len(data))
	out = append(out, 0xAA)
	out = append(out, data...)
	return out, nil
}

func (c *testCompressor) Decompress(data []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(data) == 0 || data[0] != 0xAA {
		return nil, errors.New("invalid compressed data")
	}
	out := make([]byte, len(data)-1)
	copy(out, data[1:])
	return out, nil
}

func (c *testCompressor) ContentEncoding() string { return "test-magic" }

// ---------- Tests ----------

func TestS3_PutAndGet(t *testing.T) {
	client := newMemS3()
	store := New(Config{
		Bucket: "test-bucket",
		Client: client,
	})
	ctx := context.Background()

	data := []byte(`{"entity":"user","version":1}`)
	key := "user/u1/1"

	if err := store.Put(ctx, key, data); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("Get = %s, want %s", got, data)
	}
}

func TestS3_PutAndGetWithCompression(t *testing.T) {
	client := newMemS3()
	comp := &testCompressor{}
	store := New(Config{
		Bucket:     "test-bucket",
		Client:     client,
		Compressor: comp,
	})
	ctx := context.Background()

	data := []byte(`{"compressed":"true"}`)
	key := "user/u2/1"

	if err := store.Put(ctx, key, data); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Verify stored data is compressed (has magic byte).
	client.mu.Lock()
	stored := client.objects["test-bucket/"+key]
	client.mu.Unlock()

	if len(stored) == 0 || stored[0] != 0xAA {
		t.Error("stored data should be compressed (have magic byte)")
	}

	// Get should decompress.
	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("Get = %s, want %s", got, data)
	}
}

func TestS3_GetNotFound(t *testing.T) {
	client := newMemS3()
	store := New(Config{
		Bucket: "test-bucket",
		Client: client,
	})
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent/key")
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
}

func TestS3_MultipleKeys(t *testing.T) {
	client := newMemS3()
	store := New(Config{
		Bucket: "test-bucket",
		Client: client,
	})
	ctx := context.Background()

	keys := []string{"user/u1/1", "user/u1/2", "order/o1/1"}
	for i, key := range keys {
		data := []byte(fmt.Sprintf(`{"version":%d}`, i+1))
		if err := store.Put(ctx, key, data); err != nil {
			t.Fatalf("Put(%s): %v", key, err)
		}
	}

	for i, key := range keys {
		got, err := store.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get(%s): %v", key, err)
		}
		expected := []byte(fmt.Sprintf(`{"version":%d}`, i+1))
		if !bytes.Equal(got, expected) {
			t.Errorf("Get(%s) = %s, want %s", key, got, expected)
		}
	}
}

func TestS3_OverwriteKey(t *testing.T) {
	client := newMemS3()
	store := New(Config{
		Bucket: "test-bucket",
		Client: client,
	})
	ctx := context.Background()

	key := "user/u1/1"
	_ = store.Put(ctx, key, []byte("v1"))
	_ = store.Put(ctx, key, []byte("v2"))

	got, _ := store.Get(ctx, key)
	if string(got) != "v2" {
		t.Errorf("Get after overwrite = %s, want v2", got)
	}
}

func TestS3_LargePayload(t *testing.T) {
	client := newMemS3()
	store := New(Config{
		Bucket: "test-bucket",
		Client: client,
	})
	ctx := context.Background()

	// 1MB payload.
	data := make([]byte, 1<<20)
	for i := range data {
		data[i] = byte(i % 256)
	}

	key := "large/payload/1"
	if err := store.Put(ctx, key, data); err != nil {
		t.Fatalf("Put large: %v", err)
	}

	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get large: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Error("large payload round-trip mismatch")
	}
}

func TestS3_ConcurrentPutGet(t *testing.T) {
	client := newMemS3()
	store := New(Config{
		Bucket: "test-bucket",
		Client: client,
	})
	ctx := context.Background()

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make(chan error, goroutines*2)

	// Concurrent puts.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("concurrent/%d", id)
			data := []byte(fmt.Sprintf(`{"id":%d}`, id))
			if err := store.Put(ctx, key, data); err != nil {
				errs <- fmt.Errorf("Put(%s): %v", key, err)
			}
		}(i)
	}
	wg.Wait()

	// Concurrent gets.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("concurrent/%d", id)
			expected := []byte(fmt.Sprintf(`{"id":%d}`, id))
			got, err := store.Get(ctx, key)
			if err != nil {
				errs <- fmt.Errorf("Get(%s): %v", key, err)
				return
			}
			if !bytes.Equal(got, expected) {
				errs <- fmt.Errorf("Get(%s) = %s, want %s", key, got, expected)
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

func TestS3_PassthroughCompressor(t *testing.T) {
	p := &PassthroughCompressor{}
	data := []byte("hello world passthrough test data 12345")

	compressed, err := p.Compress(data)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}

	decompressed, err := p.Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}

	if !bytes.Equal(decompressed, data) {
		t.Errorf("round-trip failed: got %s, want %s", decompressed, data)
	}

	if p.ContentEncoding() != "identity" {
		t.Errorf("ContentEncoding = %s, want identity", p.ContentEncoding())
	}
}

func TestS3_EmptyData(t *testing.T) {
	client := newMemS3()
	store := New(Config{
		Bucket: "test-bucket",
		Client: client,
	})
	ctx := context.Background()

	key := "empty/data"
	if err := store.Put(ctx, key, []byte{}); err != nil {
		t.Fatalf("Put empty: %v", err)
	}

	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d bytes", len(got))
	}
}

func BenchmarkS3_Put(b *testing.B) {
	client := newMemS3()
	store := New(Config{
		Bucket: "bench-bucket",
		Client: client,
	})
	ctx := context.Background()
	data := []byte(`{"name":"benchmark","version":1}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.Put(ctx, fmt.Sprintf("bench/%d", i), data)
	}
}
