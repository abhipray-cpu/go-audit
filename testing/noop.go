package audittest

import (
	"context"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time interface checks for noop adapters.
var _ port.WALPort = (*NoopWAL)(nil)
var _ port.CachePort = (*NoopCache)(nil)
var _ port.InstrumenterPort = (*NoopInstrumenter)(nil)
var _ port.SubscriberPort = (*NoopSubscriber)(nil)

// NoopWAL is a no-operation implementation of [port.WALPort].
// Use it in tests or configurations where WAL is not needed.
type NoopWAL struct{}

// Append does nothing and returns nil.
func (n *NoopWAL) Append(_ context.Context, _ port.WALEntry) error { return nil }

// Ack does nothing and returns nil.
func (n *NoopWAL) Ack(_ context.Context, _ string) error { return nil }

// Replay always returns an empty slice and nil error.
func (n *NoopWAL) Replay(_ context.Context) ([]port.WALEntry, error) { return nil, nil }

// NoopCache is a no-operation implementation of [port.CachePort].
// Get always misses, Put and Invalidate are silent.
type NoopCache struct{}

// Get always returns a zero record and false (cache miss).
func (n *NoopCache) Get(_ context.Context, _, _ string, _ int64) (domain.VersionRecord, bool) {
	return domain.VersionRecord{}, false
}

// Put does nothing.
func (n *NoopCache) Put(_ context.Context, _ domain.VersionRecord) {}

// Invalidate does nothing.
func (n *NoopCache) Invalidate(_ context.Context, _, _ string) {}

// NoopInstrumenter is a no-operation implementation of [port.InstrumenterPort].
// Spans are inert and metrics are discarded.
type NoopInstrumenter struct{}

// StartSpan returns the context unchanged and an inert span.
func (n *NoopInstrumenter) StartSpan(ctx context.Context, _ string) (context.Context, port.Span) {
	return ctx, &noopSpan{}
}

// EndSpan does nothing.
func (n *NoopInstrumenter) EndSpan(_ port.Span) {}

// RecordMetric does nothing.
func (n *NoopInstrumenter) RecordMetric(_ context.Context, _ string, _ float64, _ map[string]string) {
}

// noopSpan is an inert span used by [NoopInstrumenter].
type noopSpan struct{}

func (s *noopSpan) End()                         {}
func (s *noopSpan) SetAttribute(_ string, _ any) {}

// NoopSubscriber is a no-operation implementation of [port.SubscriberPort].
// OnVersion always returns nil.
type NoopSubscriber struct{}

// OnVersion does nothing and returns nil.
func (n *NoopSubscriber) OnVersion(_ context.Context, _ domain.VersionRecord) error { return nil }
