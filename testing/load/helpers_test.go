//go:build loadtest

package load

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"net"
	"os"
	"testing"
	"time"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/adapter/logger"
	mysqladapter "github.com/abhipray-cpu/go-audit/adapter/mysql"
	"github.com/abhipray-cpu/go-audit/adapter/postgres"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
)

// ---------------------------------------------------------------------------
// Connection helpers — identical to integration/ helpers but scoped to this
// module so that testing/load has no compile-time dependency on integration.
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

func skipIfNoDocker(t *testing.T) {
	t.Helper()
	// Check TCP connectivity to Postgres as a proxy for Docker availability.
	conn, err := net.DialTimeout("tcp", "localhost:5432", 2*time.Second)
	if err != nil {
		t.Skip("Docker services not available — skipping")
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

// ---------------------------------------------------------------------------
// Shared entity types for load tests.
// ---------------------------------------------------------------------------

// UserEntity mirrors the entity used in integration tests.
type UserEntity struct {
	ID       string            `version:"id"`
	Name     string            `version:"tracked"`
	Email    string            `version:"tracked"`
	Age      int               `version:"tracked"`
	Roles    []string          `version:"normalized"`
	Settings map[string]string `version:"unordered"`
}
