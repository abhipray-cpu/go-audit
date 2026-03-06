package otel

import (
	"context"
	"testing"
)

func TestOTELInstrumenter_StartSpan(t *testing.T) {
	// Uses the global noop TracerProvider/MeterProvider by default.
	inst := New(Config{ServiceName: "test-audit"})

	ctx, span := inst.StartSpan(context.Background(), "audit.version")
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if span == nil {
		t.Fatal("expected non-nil span")
	}

	span.SetAttribute("entity_type", "user")
	span.SetAttribute("version", 42)
	span.SetAttribute("latency", 1.5)
	span.SetAttribute("async", true)

	inst.EndSpan(span)
}

func TestOTELInstrumenter_RecordMetric(t *testing.T) {
	inst := New(Config{ServiceName: "test-audit"})

	// Should not panic even with the noop meter.
	inst.RecordMetric(context.Background(), "audit.version.duration", 42.0, map[string]string{
		"entity_type": "user",
		"strategy":    "full",
	})

	// Record the same metric again — counter is reused.
	inst.RecordMetric(context.Background(), "audit.version.duration", 10.0, nil)
}

func TestOTELInstrumenter_AllMetricsDefined(t *testing.T) {
	if len(MetricNames) < 24 {
		t.Fatalf("expected at least 24 metric names, got %d", len(MetricNames))
	}
}

func TestOTELInstrumenter_NilSpan(t *testing.T) {
	inst := New(Config{})
	// EndSpan with nil should not panic.
	inst.EndSpan(nil)
}

func TestOTELInstrumenter_NoOTELInCoreGoMod(t *testing.T) {
	// This test documents the architectural invariant: the core go.mod
	// at the repo root must NOT import go.opentelemetry.io. The OTEL
	// plugin lives in a separate sub-module. This is verified by CI.
	// Here we simply assert the MetricNames constant is accessible from
	// this sub-module.
	if MetricNames[0] != "audit.version.duration" {
		t.Fatalf("unexpected first metric: %s", MetricNames[0])
	}
}
