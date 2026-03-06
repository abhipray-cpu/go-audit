package port

import (
	"context"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
)

// Transaction is an opaque handle to a caller-managed database transaction.
// Adapters perform a type assertion to their concrete transaction type
// (e.g., *sql.Tx for PostgreSQL).
type Transaction interface{}

// ListOption configures pagination and ordering for list queries.
type ListOption func(*ListOptions)

// ListOptions holds the resolved pagination and ordering parameters.
type ListOptions struct {
	// Limit is the maximum number of records to return.
	Limit int

	// Offset is the number of records to skip.
	Offset int

	// OrderAsc indicates ascending order when true, descending when false.
	OrderAsc bool
}

// DefaultListOptions returns sensible defaults for list queries.
func DefaultListOptions() ListOptions {
	return ListOptions{
		Limit:    50,
		Offset:   0,
		OrderAsc: false,
	}
}

// ApplyListOptions applies the given options to the defaults.
func ApplyListOptions(opts ...ListOption) ListOptions {
	o := DefaultListOptions()
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

// WithLimit sets the maximum number of records to return.
func WithLimit(n int) ListOption {
	return func(o *ListOptions) {
		o.Limit = n
	}
}

// WithOffset sets the number of records to skip.
func WithOffset(n int) ListOption {
	return func(o *ListOptions) {
		o.Offset = n
	}
}

// WithOrderAsc sets ascending order.
func WithOrderAsc() ListOption {
	return func(o *ListOptions) {
		o.OrderAsc = true
	}
}

// WithOrderDesc sets descending order.
func WithOrderDesc() ListOption {
	return func(o *ListOptions) {
		o.OrderAsc = false
	}
}

// VersionOption configures the behavior of a Version() call.
type VersionOption func(*VersionOptions)

// VersionOptions holds the resolved options for a Version() call.
type VersionOptions struct {
	// Sync forces synchronous (blocking) mode instead of background processing.
	Sync bool

	// Strategy overrides the default storage strategy for this version.
	Strategy *domain.Strategy
}

// WithSync forces the version to be created synchronously.
func WithSync() VersionOption {
	return func(o *VersionOptions) {
		o.Sync = true
	}
}

// WithStrategy overrides the storage strategy for this single version.
func WithStrategy(s domain.Strategy) VersionOption {
	return func(o *VersionOptions) {
		o.Strategy = &s
	}
}

// Span represents an in-flight observability span.
type Span interface {
	// End completes the span.
	End()

	// SetAttribute attaches a key-value attribute to the span.
	SetAttribute(key string, value any)
}

// HealthStatus reports the health of a single backend.
type HealthStatus struct {
	// Name identifies the backend (e.g., "postgres", "clickhouse").
	Name string

	// Healthy indicates whether the backend is reachable and operational.
	Healthy bool

	// Latency is the round-trip time of the health check probe.
	Latency time.Duration

	// Error is the error message if the backend is unhealthy.
	Error string
}

// WALEntry represents a single entry in the Write-Ahead Log.
type WALEntry struct {
	// ID is the unique identifier for this WAL entry.
	ID string

	// Record is the version record to be persisted.
	Record domain.VersionRecord

	// CreatedAt is when the entry was appended to the WAL.
	CreatedAt time.Time
}

// MigrationFunc transforms a version record from one schema version to another.
type MigrationFunc func(ctx context.Context, data []byte) ([]byte, error)
