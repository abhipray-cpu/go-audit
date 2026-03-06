package gcs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
)

// ---------- in-memory GCS mock ----------

type memGCS struct {
	mu      sync.Mutex
	objects map[string][]byte // bucket/key → data
}

func newMemGCS() *memGCS {
	return &memGCS{objects: make(map[string][]byte)}
}

func (m *memGCS) Write(_ context.Context, bucket, key string, data io.Reader) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	b, err := io.ReadAll(data)
	if err != nil {
		return err
	}

	m.objects[bucket+"/"+key] = b
	return nil
}

func (m *memGCS) Read(_ context.Context, bucket, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, ok := m.objects[bucket+"/"+key]
	if !ok {
		return nil, fmt.Errorf("storage: object doesn't exist: %s/%s", bucket, key)
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}

// ---------- Tests ----------

func TestGCS_PutAndGet(t *testing.T) {
	client := newMemGCS()
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

func TestGCS_PutAndGetWithCompression(t *testing.T) {
	client := newMemGCS()
	store := New(Config{
		Bucket:     "test-bucket",
		Client:     client,
		Compressor: &testCompressor{},
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

	if len(stored) == 0 || stored[0] != 0xBB {
		t.Error("stored data should be compressed (have magic byte)")
	}

	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("Get = %s, want %s", got, data)
	}
}

func TestGCS_GetNotFound(t *testing.T) {
	client := newMemGCS()
	store := New(Config{
		Bucket: "test-bucket",
		Client: client,
	})
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent/key")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestGCS_MultipleKeys(t *testing.T) {
	client := newMemGCS()
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

func TestGCS_OverwriteKey(t *testing.T) {
	client := newMemGCS()
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

func TestGCS_LargePayload(t *testing.T) {
	client := newMemGCS()
	store := New(Config{
		Bucket: "test-bucket",
		Client: client,
	})
	ctx := context.Background()

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

func TestGCS_ConcurrentPutGet(t *testing.T) {
	client := newMemGCS()
	store := New(Config{
		Bucket: "test-bucket",
		Client: client,
	})
	ctx := context.Background()

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make(chan error, goroutines*2)

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

func TestGCS_EmptyData(t *testing.T) {
	client := newMemGCS()
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

func BenchmarkGCS_Put(b *testing.B) {
	client := newMemGCS()
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

// ---------- test compressor ----------

type testCompressor struct {
	mu sync.Mutex
}

func (c *testCompressor) Compress(data []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]byte, 0, 1+len(data))
	out = append(out, 0xBB)
	out = append(out, data...)
	return out, nil
}

func (c *testCompressor) Decompress(data []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(data) == 0 || data[0] != 0xBB {
		return nil, fmt.Errorf("invalid compressed data")
	}
	out := make([]byte, len(data)-1)
	copy(out, data[1:])
	return out, nil
}
