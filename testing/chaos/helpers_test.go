//go:build chaos

package chaos

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	audit "github.com/abhipray-cpu/go-audit"
	s3store "github.com/abhipray-cpu/go-audit/adapter/coldstore/s3"
	"github.com/abhipray-cpu/go-audit/adapter/logger"
	mysqladapter "github.com/abhipray-cpu/go-audit/adapter/mysql"
	"github.com/abhipray-cpu/go-audit/adapter/postgres"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
)

// ---------------------------------------------------------------------------
// Connection & utility helpers
// ---------------------------------------------------------------------------

func postgresDSN() string {
	if v := os.Getenv("AUDIT_TEST_POSTGRES_DSN"); v != "" {
		return v
	}
	return "postgres://audit:audit@localhost:5432/audit_test?sslmode=disable"
}

func mysqlDSN() string {
	if v := os.Getenv("AUDIT_TEST_MYSQL_DSN"); v != "" {
		return v
	}
	return "audit:audit@tcp(localhost:3306)/audit_test?parseTime=true"
}

func minioEndpoint() string {
	if v := os.Getenv("AUDIT_TEST_MINIO_ENDPOINT"); v != "" {
		return v
	}
	return "localhost:9100"
}

func minioAccessKey() string {
	if v := os.Getenv("AUDIT_TEST_MINIO_ACCESS_KEY"); v != "" {
		return v
	}
	return "minioadmin"
}

func minioSecretKey() string {
	if v := os.Getenv("AUDIT_TEST_MINIO_SECRET_KEY"); v != "" {
		return v
	}
	return "minioadmin"
}

func skipIfNoDocker(t *testing.T) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", "localhost:5432", 2*time.Second)
	if err != nil {
		t.Skip("Docker services not available")
	}
	conn.Close()
}

func connectPostgres(t *testing.T) *pgx.Conn {
	t.Helper()
	skipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, postgresDSN())
	if err != nil {
		t.Skipf("Postgres not reachable: %v", err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	return conn
}

// sqlMySQLDB wraps *sql.DB to satisfy mysqladapter.MySQLDB interface.
type sqlMySQLDB struct {
	db *sql.DB
}

func (s *sqlMySQLDB) ExecContext(ctx context.Context, query string, args ...any) (mysqladapter.MySQLResult, error) {
	r, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *sqlMySQLDB) QueryContext(ctx context.Context, query string, args ...any) (mysqladapter.MySQLRows, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *sqlMySQLDB) QueryRowContext(ctx context.Context, query string, args ...any) mysqladapter.MySQLRow {
	return s.db.QueryRowContext(ctx, query, args...)
}

func (s *sqlMySQLDB) Close() error { return s.db.Close() }

func connectMySQL(t *testing.T) *sqlMySQLDB {
	t.Helper()
	skipIfNoDocker(t)
	db, err := sql.Open("mysql", mysqlDSN())
	if err != nil {
		t.Skipf("MySQL open: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Skipf("MySQL not reachable: %v", err)
	}
	return &sqlMySQLDB{db: db}
}

func cleanupPGTables(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	_, _ = conn.Exec(ctx, "DROP TABLE IF EXISTS audit_versions CASCADE")
	_, _ = conn.Exec(ctx, "DROP TABLE IF EXISTS audit_schema_version CASCADE")
	if err := postgres.AutoMigrate(ctx, conn); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
}

func cleanupMySQL(t *testing.T, db mysqladapter.MySQLDB) {
	t.Helper()
	ctx := context.Background()
	_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS audit_versions")
	_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS audit_schema_version")
	if err := mysqladapter.AutoMigrate(ctx, db); err != nil {
		t.Fatalf("MySQL AutoMigrate: %v", err)
	}
}

func newAuditorPG(t *testing.T, conn *pgx.Conn, opts ...func(*audit.Config)) *audit.Auditor {
	t.Helper()
	w := postgres.NewWriter(conn)
	r := postgres.NewReader(conn)
	cfg := audit.Config{Writer: w, Reader: r, Logger: logger.NewSlog(nil)}
	for _, opt := range opts {
		opt(&cfg)
	}
	a, err := audit.New(cfg)
	if err != nil {
		t.Fatalf("New auditor: %v", err)
	}
	t.Cleanup(func() { a.Shutdown(context.Background()) })
	return a
}

func versionSync(t *testing.T, ctx context.Context, a *audit.Auditor, entity any) domain.VersionRecord {
	t.Helper()
	pv, err := a.Version(ctx, entity, port.WithSync())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	rec, err := pv.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	return rec
}

var uniqueCounter int64

func uniqueID(prefix string) string {
	uniqueCounter++
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), uniqueCounter)
}

func randString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// UserEntity mirrors the entity used in integration tests.
type UserEntity struct {
	ID       string            `version:"id"`
	Name     string            `version:"tracked"`
	Email    string            `version:"tracked"`
	Age      int               `version:"tracked"`
	Roles    []string          `version:"normalized"`
	Settings map[string]string `version:"unordered"`
}

// ---------------------------------------------------------------------------
// MinIO S3 client for cold store chaos tests
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

func (m *minioS3Client) signV4(req *http.Request, payload []byte) {
	now := time.Now().UTC()
	datestamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	region := "us-east-1"
	service := "s3"
	req.Header.Set("x-amz-date", amzDate)
	payloadHash := sha256Hex(payload)
	req.Header.Set("x-amz-content-sha256", payloadHash)
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
		canonicalHeaders.WriteString(k + ":" + req.Header.Get(k) + "\n")
	}
	canonicalRequest := strings.Join([]string{
		req.Method, req.URL.Path, req.URL.RawQuery,
		canonicalHeaders.String(), signedHeaders, payloadHash,
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
	return hmacSHA256(kService, []byte("aws4_request"))
}

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
