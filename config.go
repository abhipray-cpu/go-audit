package audit

import (
	"fmt"
	"time"

	"github.com/abhipray-cpu/go-audit/adapter/cloner"
	"github.com/abhipray-cpu/go-audit/adapter/hash"
	"github.com/abhipray-cpu/go-audit/adapter/logger"
	"github.com/abhipray-cpu/go-audit/adapter/metadata"
	"github.com/abhipray-cpu/go-audit/adapter/serializer"
	"github.com/abhipray-cpu/go-audit/adapter/subscriber"
	walfile "github.com/abhipray-cpu/go-audit/adapter/wal/file"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/diff"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Config holds the configuration for an [Auditor] instance.
//
// Only [Config.Writer] and [Config.Reader] are required (they carry the
// database backend). All other fields are optional — sensible defaults are
// wired automatically:
//
//   - Logger:     slog default logger adapter
//   - Serializer: JSON (encoding/json)
//   - Cloner:     reflection-based deep copy
//   - Differ:     structural diff engine
//
// Example:
//
//	auditor, err := audit.New(audit.Config{
//	    Writer: pgWriter,
//	    Reader: pgReader,
//	})
type Config struct {
	// Writer is the primary storage writer (required).
	Writer port.VersionWriterPort

	// Reader is the primary storage reader (required).
	Reader port.VersionReaderPort

	// Logger is used for structured internal logging.
	// Default: slog adapter wrapping slog.Default().
	Logger port.LoggerPort

	// Serializer marshals/unmarshals entity data for storage.
	// Default: JSONSerializer.
	Serializer port.SerializerPort

	// Cloner creates deep copies of entities before background processing.
	// Default: ReflectCloner.
	Cloner port.ClonerPort

	// Differ computes structural diffs between entity versions.
	// Default: diff.Engine.
	Differ port.DifferPort

	// MetadataExtractor extracts audit metadata from the request context.
	// Default: ContextExtractor wrapping [MetadataFromCtx].
	MetadataExtractor port.MetadataExtractorPort

	// PoolWorkers is the number of background worker goroutines.
	// Default: runtime.NumCPU() * 2.
	//
	// ⚠️  Background mode requires a connection pool (e.g., *pgxpool.Pool)
	// as the database handle. A single *pgx.Conn is NOT goroutine-safe and
	// will cause "conn busy" panics when workers issue concurrent queries.
	PoolWorkers int

	// PoolQueueSize is the capacity of the background work channel.
	// Default: 10,000.
	PoolQueueSize int

	// Strategy is the default storage strategy for version records.
	// Default: StrategyFull (complete snapshots on every version).
	// Set to StrategyDelta for delta-only or StrategyHybrid for hybrid mode.
	Strategy domain.Strategy

	// HybridSnapshotInterval controls how often a full snapshot is stored
	// when using StrategyHybrid. Every N-th version is a full snapshot;
	// all others are deltas. Must be ≥ 2. Default: 10.
	HybridSnapshotInterval int

	// WAL is the Write-Ahead Log adapter. When set, version records are
	// appended to the WAL before entering the worker queue and acknowledged
	// after successful persistence. On startup, un-acknowledged entries are
	// replayed to recover in-flight work.
	// Default: nil (WAL disabled — no crash recovery).
	WAL port.WALPort

	// WALDir is a convenience field: when set and WAL is nil, a [walfile.WAL]
	// is automatically created at this directory path. Ignored if WAL is
	// already set.
	WALDir string

	// MaxEntitySizeBytes is the maximum allowed serialized size of an entity
	// at runtime (before persistence). Entities exceeding this limit are
	// rejected with [domain.ErrEntityTooLarge]. Set to 0 to disable the
	// runtime size check. Default: 0 (disabled).
	MaxEntitySizeBytes int64

	// MaxMetadataSizeBytes is the maximum allowed JSON-encoded size of
	// [domain.VersionMetadata] at runtime. Metadata exceeding this limit
	// is rejected with [domain.ErrMetadataTooLarge]. Set to 0 to disable
	// the runtime size check. Default: 0 (disabled).
	MaxMetadataSizeBytes int64

	// Subscriber receives post-persist notifications for every version
	// record. Use [subscriber.Fanout] to broadcast to multiple subscribers.
	// Default: [subscriber.LogSubscriber] wrapping the configured Logger.
	Subscriber port.SubscriberPort

	// OLAPBatchSize is the number of records that triggers an immediate
	// flush to the OLAP backend. Only meaningful when an OLAP adapter is
	// configured. Default: 1000.
	OLAPBatchSize int

	// OLAPFlushInterval is the maximum time between OLAP flushes even when
	// the batch size has not been reached. Default: 5s.
	OLAPFlushInterval time.Duration

	// EnableHashChain enables SHA-256 hash chain verification. When true,
	// every persisted version record includes a hash that chains to the
	// previous version's hash, creating a tamper-evident audit trail.
	// Default: false.
	EnableHashChain bool

	// Hasher is the hash computer used for hash chain computation.
	// Default: SHA-256 (only used when EnableHashChain is true).
	Hasher port.HashComputerPort

	// RetentionDays maps entity type names to their retention period in days.
	// Versions older than the retention period are eligible for background
	// cleanup. Set to 0 or omit to retain indefinitely. Default: nil.
	RetentionDays map[string]int

	// Hooks is the hooks registry for authorization and lifecycle callbacks.
	// When set, hooks are executed before reads and writes.
	// Default: nil (no hooks).
	Hooks port.HooksPort

	// Clock provides the current time. Swap in a mock for deterministic
	// time in tests. Default: system clock (time.Now).
	Clock port.ClockPort
}

// validate checks that the required fields are present and returns a
// descriptive [domain.ErrConfiguration] on failure.
func (c *Config) validate() error {
	if c.Writer == nil {
		return fmt.Errorf("%w: Writer is required", domain.ErrConfiguration)
	}
	if c.Reader == nil {
		return fmt.Errorf("%w: Reader is required", domain.ErrConfiguration)
	}
	return nil
}

// applyDefaults fills nil optional fields with production-quality defaults.
func (c *Config) applyDefaults() {
	if c.Logger == nil {
		c.Logger = logger.NewSlog(nil) // wraps slog.Default()
	}
	if c.Serializer == nil {
		c.Serializer = serializer.NewJSON()
	}
	if c.Cloner == nil {
		c.Cloner = cloner.NewReflect()
	}
	if c.Differ == nil {
		c.Differ = diff.New()
	}
	if c.MetadataExtractor == nil {
		c.MetadataExtractor = metadata.NewContextExtractor(MetadataFromCtx)
	}
	if c.WAL == nil && c.WALDir != "" {
		w, err := walfile.New(c.WALDir)
		if err == nil {
			c.WAL = w
		}
		// If err != nil, WAL stays nil — no crash recovery, but no startup failure.
	}
	if c.Subscriber == nil {
		c.Subscriber = subscriber.NewLog(c.Logger)
	}
	if c.EnableHashChain && c.Hasher == nil {
		c.Hasher = hash.NewSHA256()
	}
	if c.Clock == nil {
		c.Clock = systemClock{}
	}
}

// systemClock is the default ClockPort using the real system time.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }
