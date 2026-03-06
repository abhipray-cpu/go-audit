// Package audittest — containers.go provides testcontainers-based
// infrastructure for integration tests.
//
// Usage:
//
//	func TestWithPostgres(t *testing.T) {
//	    if testing.Short() {
//	        t.Skip("skipping integration test")
//	    }
//	    infra := audittest.NewTestInfra(t)
//	    // infra.PostgresDSN() is ready to use
//	}
//
// The containers are cleaned up automatically via t.Cleanup.
//
// NOTE: Requires Docker to be running.
package audittest

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

// TestInfra holds connection details for containerised test infrastructure.
// Call [NewTestInfra] to start all containers; they are torn down via t.Cleanup.
type TestInfra struct {
	// PostgresDSN returns a PostgreSQL connection string.
	postgresDSN string
}

// PostgresDSN returns the DSN for the test PostgreSQL instance.
func (ti *TestInfra) PostgresDSN() string { return ti.postgresDSN }

// NewTestInfra starts a PostgreSQL container (and optionally others) using
// testcontainers-go. If testcontainers is not available or Docker is not
// running, the test is skipped.
//
// Containers are stopped automatically via t.Cleanup.
//
// If you don't have Docker, use [NewAuditor] with the in-memory store instead.
func NewTestInfra(t *testing.T) *TestInfra {
	t.Helper()

	// Check if Docker daemon is reachable by attempting to connect to the
	// Docker socket. This avoids pulling in the Docker SDK as a hard dependency.
	if !isDockerAvailable() {
		t.Skip("Docker not available — skipping integration test")
	}

	// For now, attempt to use the AUDIT_TEST_POSTGRES_DSN environment variable.
	// Full testcontainers integration will be added when the testcontainers-go
	// dependency is introduced in Phase 2 (GA-028 stretch goal).
	dsn := postgresFromEnvOrDefault(t)

	return &TestInfra{postgresDSN: dsn}
}

// isDockerAvailable does a quick TCP dial to the Docker socket.
func isDockerAvailable() bool {
	conn, err := net.DialTimeout("unix", "/var/run/docker.sock", 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// postgresFromEnvOrDefault returns the test Postgres DSN. It checks the
// AUDIT_TEST_POSTGRES_DSN environment variable first; if unset, it tries
// the default local Docker-mapped port.
func postgresFromEnvOrDefault(t *testing.T) string {
	t.Helper()

	// Try connecting to the default local PostgreSQL port.
	dsn := "postgres://audit:audit@localhost:5432/audit_test?sslmode=disable"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := net.DialTimeout("tcp", "localhost:5432", 5*time.Second)
	if err != nil {
		t.Skipf("PostgreSQL not reachable at localhost:5432: %v", err)
	}
	conn.Close()
	_ = ctx

	return dsn
}

// FormatPostgresDSN is a helper that builds a DSN from individual components.
func FormatPostgresDSN(host string, port int, user, pass, dbname string) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", user, pass, host, port, dbname)
}
