//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/abhipray-cpu/go-audit/adapter/health"
)

// ---------------------------------------------------------------------------
// Phase 2 — HC-001 to HC-003: Health Checks
// ---------------------------------------------------------------------------

// HC-001: TestHealthCheck_AllHealthy verifies all probes pass for Docker backends.
func TestHealthCheck_AllHealthy(t *testing.T) {
	conn := connectPostgres(t)

	checker := health.New(
		health.Probe{
			Name: "postgres",
			Check: func(ctx context.Context) error {
				return conn.Ping(ctx)
			},
		},
	)

	results := checker.Check(context.Background())
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Healthy {
		t.Fatalf("postgres should be healthy: %v", results[0].Error)
	}
}

// HC-002: TestHealthCheck_Latency verifies latency is measured and > 0.
func TestHealthCheck_Latency(t *testing.T) {
	conn := connectPostgres(t)

	checker := health.New(
		health.Probe{
			Name: "postgres",
			Check: func(ctx context.Context) error {
				return conn.Ping(ctx)
			},
		},
	)

	results := checker.Check(context.Background())
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if results[0].Latency <= 0 {
		t.Fatalf("expected positive latency, got %v", results[0].Latency)
	}
}

// HC-003: TestHealthCheck_UnhealthyBackend verifies failed probe returns unhealthy.
func TestHealthCheck_UnhealthyBackend(t *testing.T) {
	checker := health.New(
		health.Probe{
			Name: "fake-db",
			Check: func(ctx context.Context) error {
				return errors.New("connection refused")
			},
		},
	)

	results := checker.Check(context.Background())
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Healthy {
		t.Fatal("fake-db should be unhealthy")
	}
	if results[0].Error == "" {
		t.Fatal("expected error message for unhealthy backend")
	}
}
