// Package otel provides the OpenTelemetry instrumentation plugin for go-audit.
//
// This plugin implements [port.InstrumenterPort] with OpenTelemetry spans and
// metrics. It is distributed as a separate Go sub-module to keep OTEL
// dependencies out of the core library.
//
//	import "github.com/abhipray-cpu/go-audit/plugin/otel"
//
//	instrumenter := otel.New(otel.Config{ServiceName: "my-service"})
//	auditor, _ := audit.New(audit.Config{Instrumenter: instrumenter, ...})
package otel

import (
	"context"
	"sync"
	"time"

	"github.com/abhipray-cpu/go-audit/domain/port"
	otelapi "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Compile-time check.
var _ port.InstrumenterPort = (*Instrumenter)(nil)

// Config configures the OTEL [Instrumenter].
type Config struct {
	// ServiceName is used as the tracer/meter namespace.
	// Default: "go-audit".
	ServiceName string

	// TracerProvider overrides the global TracerProvider.
	// Default: otel.GetTracerProvider().
	TracerProvider trace.TracerProvider

	// MeterProvider overrides the global MeterProvider.
	// Default: otel.GetMeterProvider().
	MeterProvider metric.MeterProvider
}

// Instrumenter implements [port.InstrumenterPort] with OpenTelemetry.
type Instrumenter struct {
	tracer  trace.Tracer
	meter   metric.Meter
	metrics map[string]metric.Float64Counter
	mu      sync.RWMutex
}

// New creates an OTEL [Instrumenter].
func New(cfg Config) *Instrumenter {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "go-audit"
	}
	tp := cfg.TracerProvider
	if tp == nil {
		tp = otelapi.GetTracerProvider()
	}
	mp := cfg.MeterProvider
	if mp == nil {
		mp = otelapi.GetMeterProvider()
	}

	return &Instrumenter{
		tracer:  tp.Tracer(cfg.ServiceName),
		meter:   mp.Meter(cfg.ServiceName),
		metrics: make(map[string]metric.Float64Counter),
	}
}

// otelSpan wraps an OTEL span so it satisfies [port.Span].
type otelSpan struct {
	span trace.Span
}

func (s *otelSpan) End() { s.span.End() }

func (s *otelSpan) SetAttribute(key string, value any) {
	switch v := value.(type) {
	case string:
		s.span.SetAttributes(attribute.String(key, v))
	case int:
		s.span.SetAttributes(attribute.Int(key, v))
	case int64:
		s.span.SetAttributes(attribute.Int64(key, v))
	case float64:
		s.span.SetAttributes(attribute.Float64(key, v))
	case bool:
		s.span.SetAttributes(attribute.Bool(key, v))
	case time.Duration:
		s.span.SetAttributes(attribute.String(key, v.String()))
	}
}

// StartSpan begins a new OTEL span.
func (i *Instrumenter) StartSpan(ctx context.Context, operationName string) (context.Context, port.Span) {
	ctx, span := i.tracer.Start(ctx, operationName)
	return ctx, &otelSpan{span: span}
}

// EndSpan completes a span.
func (i *Instrumenter) EndSpan(span port.Span) {
	if span != nil {
		span.End()
	}
}

// RecordMetric records a named metric counter with the given value and tags.
func (i *Instrumenter) RecordMetric(ctx context.Context, name string, value float64, tags map[string]string) {
	counter := i.getOrCreateCounter(name)
	attrs := make([]attribute.KeyValue, 0, len(tags))
	for k, v := range tags {
		attrs = append(attrs, attribute.String(k, v))
	}
	counter.Add(ctx, value, metric.WithAttributes(attrs...))
}

func (i *Instrumenter) getOrCreateCounter(name string) metric.Float64Counter {
	i.mu.RLock()
	c, ok := i.metrics[name]
	i.mu.RUnlock()
	if ok {
		return c
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	// Double-check after write lock.
	if c, ok = i.metrics[name]; ok {
		return c
	}

	c, _ = i.meter.Float64Counter(name)
	i.metrics[name] = c
	return c
}

// MetricNames is the canonical list of metrics emitted by go-audit.
var MetricNames = []string{
	"audit.version.duration",
	"audit.version.queue_depth",
	"audit.version.error_total",
	"audit.diff.duration",
	"audit.diff.fields_changed",
	"audit.store.write_duration",
	"audit.store.read_duration",
	"audit.store.olap.batch_size",
	"audit.store.olap.flush_duration",
	"audit.cache.hit_total",
	"audit.cache.miss_total",
	"audit.wal.append_duration",
	"audit.wal.replay_count",
	"audit.guardrails.reject_total",
	"audit.subscriber.duration",
	"audit.subscriber.error_total",
	"audit.pool.submit_total",
	"audit.pool.backpressure_total",
	"audit.pool.queue_depth",
	"audit.reconstruct.duration",
	"audit.compaction.duration",
	"audit.compaction.versions_merged",
	"audit.serialization.marshal_duration",
	"audit.serialization.unmarshal_duration",
	"audit.metadata.extract_duration",
}
