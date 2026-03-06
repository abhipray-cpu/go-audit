//go:build integration

// Package integration provides end-to-end integration tests for go-audit.
//
// These tests exercise the full library against real Docker-hosted databases
// (PostgreSQL, ClickHouse, MySQL, MinIO) and cover every functional
// requirement documented in ARCHITECTURE.md.
//
// Prerequisite: docker-compose up -d
//
// Run: go test -v -tags integration -count=1 -race ./...
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/adapter/logger"
	"github.com/abhipray-cpu/go-audit/adapter/postgres"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
	"github.com/jackc/pgx/v5"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	_ "github.com/go-sql-driver/mysql"
)

// ---------------------------------------------------------------------------
// DSN helpers
// ---------------------------------------------------------------------------

func postgresDSN() string {
	if v := os.Getenv("AUDIT_TEST_POSTGRES_DSN"); v != "" {
		return v
	}
	return "postgres://audit:audit@localhost:5432/audit_test?sslmode=disable"
}

func clickhouseDSN() string {
	if v := os.Getenv("AUDIT_TEST_CLICKHOUSE_DSN"); v != "" {
		return v
	}
	return "clickhouse://audit:audit@localhost:9000/audit_test"
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

// ---------------------------------------------------------------------------
// Connection helpers
// ---------------------------------------------------------------------------

func skipIfNoDocker(t *testing.T) {
	t.Helper()
	// Try the standard Docker socket first, then common alternatives (Colima, Rancher, etc.)
	sockets := []string{
		"/var/run/docker.sock",
		os.Getenv("DOCKER_HOST"),
	}
	// Add Colima socket path.
	if home, err := os.UserHomeDir(); err == nil {
		sockets = append(sockets, home+"/.colima/default/docker.sock")
	}

	for _, sock := range sockets {
		if sock == "" {
			continue
		}
		// Strip "unix://" prefix if present (from DOCKER_HOST).
		sock = strings.TrimPrefix(sock, "unix://")
		conn, err := net.DialTimeout("unix", sock, 2*time.Second)
		if err == nil {
			conn.Close()
			return
		}
	}
	t.Skip("Docker not available — skipping integration test")
}

func connectPostgres(t *testing.T) *pgx.Conn {
	t.Helper()
	skipIfNoDocker(t)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, postgresDSN())
	if err != nil {
		t.Skipf("cannot connect to Postgres: %v", err)
	}

	// AutoMigrate for fresh schema.
	if err := postgres.AutoMigrate(ctx, conn); err != nil {
		conn.Close(ctx)
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	t.Cleanup(func() {
		conn.Close(ctx)
	})

	return conn
}

func connectClickHouse(t *testing.T) *sql.DB {
	t.Helper()
	skipIfNoDocker(t)

	db, err := sql.Open("clickhouse", clickhouseDSN())
	if err != nil {
		t.Skipf("cannot open ClickHouse: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Skipf("cannot ping ClickHouse: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

func connectMySQL(t *testing.T) *sql.DB {
	t.Helper()
	skipIfNoDocker(t)

	db, err := sql.Open("mysql", mysqlDSN())
	if err != nil {
		t.Skipf("cannot open MySQL: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Skipf("cannot ping MySQL: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

// ---------------------------------------------------------------------------
// Unique ID generation (test-scoped, collision-free)
// ---------------------------------------------------------------------------

var idCounter atomic.Int64

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), idCounter.Add(1))
}

// ---------------------------------------------------------------------------
// Mock clock
// ---------------------------------------------------------------------------

type mockClock struct {
	now atomic.Value // stores time.Time
}

func newMockClock(t time.Time) *mockClock {
	c := &mockClock{}
	c.now.Store(t)
	return c
}

func (c *mockClock) Now() time.Time {
	return c.now.Load().(time.Time)
}

func (c *mockClock) Advance(d time.Duration) {
	c.now.Store(c.Now().Add(d))
}

// ---------------------------------------------------------------------------
// Auditor factory
// ---------------------------------------------------------------------------

func newAuditorPG(t *testing.T, conn *pgx.Conn, opts ...func(*audit.Config)) *audit.Auditor {
	t.Helper()

	w := postgres.NewWriter(conn)
	r := postgres.NewReader(conn)

	cfg := audit.Config{
		Writer: w,
		Reader: r,
		Logger: logger.NewSlog(nil),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	a, err := audit.New(cfg)
	if err != nil {
		t.Fatalf("New auditor: %v", err)
	}
	t.Cleanup(func() {
		a.Shutdown(context.Background())
	})

	return a
}

// versionSync is a convenience that creates a version synchronously and waits.
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

// cleanupPGTables drops and re-creates the audit tables for a fresh test.
func cleanupPGTables(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	_, _ = conn.Exec(ctx, "DROP TABLE IF EXISTS audit_versions CASCADE")
	_, _ = conn.Exec(ctx, "DROP TABLE IF EXISTS audit_schema_version CASCADE")
	_, _ = conn.Exec(ctx, "DROP TABLE IF EXISTS audit_wal_entries CASCADE")
	if err := postgres.AutoMigrate(ctx, conn); err != nil {
		t.Fatalf("AutoMigrate after cleanup: %v", err)
	}
}

// randString creates a random string of length n.
func randString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// futureTime returns a time far in the future for GetAtTime tests.
func futureTime() time.Time {
	return time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)
}
