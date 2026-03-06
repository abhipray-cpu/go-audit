package health

import (
	"context"
	"errors"
	"testing"
)

func TestHealthCheck_AllHealthy(t *testing.T) {
	checker := New(
		Probe{Name: "postgres", Check: func(_ context.Context) error { return nil }},
		Probe{Name: "clickhouse", Check: func(_ context.Context) error { return nil }},
	)

	results := checker.Check(context.Background())
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, r := range results {
		if !r.Healthy {
			t.Errorf("expected %s to be healthy", r.Name)
		}
		if r.Error != "" {
			t.Errorf("expected no error for %s, got: %s", r.Name, r.Error)
		}
		if r.Latency < 0 {
			t.Errorf("expected non-negative latency for %s", r.Name)
		}
	}
}

func TestHealthCheck_OLAPDown(t *testing.T) {
	checker := New(
		Probe{Name: "postgres", Check: func(_ context.Context) error { return nil }},
		Probe{Name: "clickhouse", Check: func(_ context.Context) error {
			return errors.New("connection refused")
		}},
	)

	results := checker.Check(context.Background())

	pg := results[0]
	if !pg.Healthy {
		t.Error("expected postgres to be healthy")
	}

	ch := results[1]
	if ch.Healthy {
		t.Error("expected clickhouse to be unhealthy")
	}
	if ch.Error != "connection refused" {
		t.Errorf("expected 'connection refused', got: %s", ch.Error)
	}
}
