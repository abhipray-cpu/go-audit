//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	s3store "github.com/abhipray-cpu/go-audit/adapter/coldstore/s3"
)

// ---------------------------------------------------------------------------
// minioS3Client implements s3.S3API against a real MinIO instance using
// raw HTTP with AWS Signature V4 authentication.
// ---------------------------------------------------------------------------

type minioS3Client struct {
	endpoint  string
	accessKey string
	secretKey string
	client    *http.Client
}

func newMinioS3Client(endpoint, accessKey, secretKey string) *minioS3Client {
	return &minioS3Client{
		endpoint:  strings.TrimRight(endpoint, "/"),
		accessKey: accessKey,
		secretKey: secretKey,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (m *minioS3Client) PutObject(ctx context.Context, input *s3store.PutObjectInput) error {
	body, err := io.ReadAll(input.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	url := fmt.Sprintf("%s/%s/%s", m.endpoint, input.Bucket, input.Key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if input.ContentEncoding != "" {
		req.Header.Set("Content-Encoding", input.ContentEncoding)
	}
	m.signV4(req, body)

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("put %s: HTTP %d: %s", input.Key, resp.StatusCode, string(respBody))
	}
	return nil
}

func (m *minioS3Client) GetObject(ctx context.Context, input *s3store.GetObjectInput) (*s3store.GetObjectOutput, error) {
	url := fmt.Sprintf("%s/%s/%s", m.endpoint, input.Bucket, input.Key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	m.signV4(req, nil)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 404 {
		resp.Body.Close()
		return nil, fmt.Errorf("key %q not found", input.Key)
	}
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("get %s: HTTP %d: %s", input.Key, resp.StatusCode, string(respBody))
	}
	return &s3store.GetObjectOutput{Body: resp.Body}, nil
}

// signV4 adds AWS Signature V4 headers to the request for MinIO auth.
func (m *minioS3Client) signV4(req *http.Request, payload []byte) {
	now := time.Now().UTC()
	datestamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	region := "us-east-1"
	service := "s3"

	req.Header.Set("x-amz-date", amzDate)

	// Hash payload.
	payloadHash := sha256Hex(payload)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	// Canonical headers.
	host := req.URL.Host
	req.Header.Set("Host", host)

	signedHeaderKeys := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	if req.Header.Get("Content-Encoding") != "" {
		signedHeaderKeys = append(signedHeaderKeys, "content-encoding")
	}
	if req.Header.Get("Content-Type") != "" {
		signedHeaderKeys = append(signedHeaderKeys, "content-type")
	}
	sort.Strings(signedHeaderKeys)
	signedHeaders := strings.Join(signedHeaderKeys, ";")

	var canonicalHeaders strings.Builder
	for _, k := range signedHeaderKeys {
		canonicalHeaders.WriteString(k)
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(req.Header.Get(k))
		canonicalHeaders.WriteString("\n")
	}

	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.Path,
		req.URL.RawQuery,
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	credentialScope := datestamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + credentialScope + "\n" + sha256Hex([]byte(canonicalRequest))

	signingKey := getSignatureKey(m.secretKey, datestamp, region, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		m.accessKey, credentialScope, signedHeaders, signature)
	req.Header.Set("Authorization", authHeader)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func getSignatureKey(secretKey, datestamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secretKey), []byte(datestamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	return kSigning
}

// ensureBucket creates a bucket in MinIO if it doesn't exist.
func (m *minioS3Client) ensureBucket(ctx context.Context, bucket string) error {
	url := fmt.Sprintf("%s/%s", m.endpoint, bucket)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, nil)
	if err != nil {
		return err
	}
	m.signV4(req, nil)
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// 200 = created, 409 = already exists — both fine.
	if resp.StatusCode != 200 && resp.StatusCode != 409 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create bucket %s: HTTP %d: %s", bucket, resp.StatusCode, string(body))
	}
	return nil
}

// skipIfNoMinIO skips the test when the MinIO container is unreachable.
func skipIfNoMinIO(t *testing.T) {
	t.Helper()
	ep := minioEndpoint()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + ep + "/minio/health/live")
	if err != nil {
		t.Skipf("MinIO not reachable at %s: %v", ep, err)
	}
	resp.Body.Close()
}

// connectMinioS3 returns a minioS3Client and ensures the test bucket exists.
func connectMinioS3(t *testing.T, bucket string) *minioS3Client {
	t.Helper()
	skipIfNoMinIO(t)

	ep := "http://" + minioEndpoint()
	client := newMinioS3Client(ep, minioAccessKey(), minioSecretKey())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.ensureBucket(ctx, bucket); err != nil {
		t.Fatalf("ensureBucket(%s): %v", bucket, err)
	}
	return client
}

// ---------------------------------------------------------------------------
// testCompressor is a simple symmetric compressor for CS-002.
// It uses a custom content-encoding name ("x-test") to avoid Go's HTTP
// client transparently decompressing "gzip" responses from MinIO.
// ---------------------------------------------------------------------------

type testCompressor struct{}

func (g *testCompressor) Compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	// Prepend a magic marker so we can verify Decompress actually runs.
	buf.WriteByte(0xAA)
	buf.Write(data)
	return buf.Bytes(), nil
}

func (g *testCompressor) Decompress(data []byte) ([]byte, error) {
	if len(data) == 0 || data[0] != 0xAA {
		return nil, fmt.Errorf("testCompressor: missing magic byte")
	}
	return data[1:], nil
}

func (g *testCompressor) ContentEncoding() string { return "x-test" }

// ---------------------------------------------------------------------------
// CS-001: Put 10KB payload → Get returns identical bytes
// ---------------------------------------------------------------------------

// TestColdStore_MinIO_PutGet verifies that a 10KB binary payload survives
// a round-trip through the S3 cold store adapter backed by real MinIO.
func TestColdStore_MinIO_PutGet(t *testing.T) {
	bucket := "audit-test-cs001"
	mc := connectMinioS3(t, bucket)

	store := s3store.New(s3store.Config{
		Bucket: bucket,
		Client: mc,
		// nil compressor → passthrough
	})

	ctx := context.Background()
	key := uniqueID("cs001") + "/entity/v1"

	// Create a 10KB payload with non-trivial content.
	payload := make([]byte, 10*1024)
	for i := range payload {
		payload[i] = byte(i % 251) // prime-period pattern
	}

	if err := store.Put(ctx, key, payload); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if !bytes.Equal(got, payload) {
		t.Fatalf("round-trip mismatch: got %d bytes, want %d bytes", len(got), len(payload))
	}
}

// ---------------------------------------------------------------------------
// CS-002: Put with compression → Get decompresses correctly
// ---------------------------------------------------------------------------

// TestColdStore_MinIO_WithCompression verifies that the Store correctly
// delegates to the Compressor on Put and Decompress on Get, producing
// the original data.
func TestColdStore_MinIO_WithCompression(t *testing.T) {
	bucket := "audit-test-cs002"
	mc := connectMinioS3(t, bucket)

	comp := &testCompressor{}
	store := s3store.New(s3store.Config{
		Bucket:     bucket,
		Client:     mc,
		Compressor: comp,
	})

	ctx := context.Background()
	key := uniqueID("cs002") + "/entity/v1"

	payload := []byte("hello compressed cold store world — this is test data for CS-002")

	if err := store.Put(ctx, key, payload); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if !bytes.Equal(got, payload) {
		t.Fatalf("decompressed data mismatch:\n  got:  %q\n  want: %q", string(got), string(payload))
	}
}

// ---------------------------------------------------------------------------
// CS-003: Get non-existent key → error
// ---------------------------------------------------------------------------

// TestColdStore_MinIO_NotFound verifies that Get returns an error for a
// key that was never written, confirming the adapter surfaces 404 properly.
func TestColdStore_MinIO_NotFound(t *testing.T) {
	bucket := "audit-test-cs003"
	mc := connectMinioS3(t, bucket)

	store := s3store.New(s3store.Config{
		Bucket: bucket,
		Client: mc,
	})

	ctx := context.Background()
	key := uniqueID("cs003") + "/nonexistent/v99"

	_, err := store.Get(ctx, key)
	if err == nil {
		t.Fatal("expected error for non-existent key, got nil")
	}
	// Just verify we got an error — the adapter wraps it.
	t.Logf("correctly got error for missing key: %v", err)
}

// ---------------------------------------------------------------------------
// CS-004: Put same key twice → latest data wins
// ---------------------------------------------------------------------------

// TestColdStore_MinIO_Overwrite verifies that writing the same key twice
// with different data results in Get returning the most recent data,
// confirming S3 overwrite semantics.
func TestColdStore_MinIO_Overwrite(t *testing.T) {
	bucket := "audit-test-cs004"
	mc := connectMinioS3(t, bucket)

	store := s3store.New(s3store.Config{
		Bucket: bucket,
		Client: mc,
	})

	ctx := context.Background()
	key := uniqueID("cs004") + "/entity/v1"

	v1 := []byte("version-1-data")
	v2 := []byte("version-2-data-completely-different-and-longer")

	if err := store.Put(ctx, key, v1); err != nil {
		t.Fatalf("Put v1: %v", err)
	}

	// Overwrite with v2.
	if err := store.Put(ctx, key, v2); err != nil {
		t.Fatalf("Put v2: %v", err)
	}

	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if !bytes.Equal(got, v2) {
		t.Fatalf("overwrite not applied:\n  got:  %q\n  want: %q", string(got), string(v2))
	}
}
