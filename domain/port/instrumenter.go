package port

import (
	"context"
)

// InstrumenterPort provides observability: distributed tracing and metrics.
type InstrumenterPort interface {
	// StartSpan begins a new observability span with the given operation name.
	StartSpan(ctx context.Context, operationName string) (context.Context, Span)

	// EndSpan completes a span. Typically deferred after StartSpan.
	EndSpan(span Span)

	// RecordMetric records a named metric with the given value and tags.
	RecordMetric(ctx context.Context, name string, value float64, tags map[string]string)
}
