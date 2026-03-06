# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] — 2026-03-06

### Added

- **MySQL adapter** (`adapter/mysql`): Full `VersionWriterPort` and `VersionReaderPort`
  implementation with InnoDB, utf8mb4, unique version constraint, and sorted queries
- **ClickHouse schema + OLAP adapter** (`adapter/clickhouse`): DDL with
  `ReplacingMergeTree`, `ORDER BY (entity_type, entity_id, version)`, and
  `OLAPPort` implementation with batch insert and paginated queries
- **DB WAL adapter** (`adapter/wal/db`): Database-backed Write-Ahead Log with
  `Append`, `Ack`, `Replay`, and `Recover` — full `WALPort` implementation
- **S3 cold store** (`adapter/coldstore/s3`): `ColdStorePort` with Zstandard
  compression, configurable bucket/prefix, and mock-friendly `S3API` interface
- **GCS cold store** (`adapter/coldstore/gcs`): `ColdStorePort` with
  configurable compression, bucket/prefix, and mock-friendly `GCSAPI` interface
- **Hash chain verification**: SHA-256 hash chain for tamper-evident audit trails
  - `Config.EnableHashChain` and `Config.Hasher`
  - `Auditor.VerifyIntegrity()` for chain validation
- **Schema evolution**: `Auditor.RegisterMigration()` for transparent data
  migration when entity structs change across versions
- **Field redaction**: `version:"redacted"` tag support for PII/sensitive fields
- **Retention policies**: `Config.RetentionDays` per-entity-type TTL with
  background cleanup
- **Cold storage tiering**: Automatic archival of expired versions to S3/GCS
  cold stores
- **Authorization hooks**: `BeforeRead`, `BeforeWrite` lifecycle hooks via
  `domain/hooks.Registry`
- **Multi-tenancy**: `WithTenant()` context helper and tenant-scoped queries
- **7 sub-modules** with independent `go.mod` files:
  - `adapter/postgres` (pgx/v5)
  - `adapter/clickhouse`
  - `adapter/mysql`
  - `adapter/coldstore/s3`
  - `adapter/coldstore/gcs`
  - `adapter/serializer/msgpack`
  - `plugin/otel`
- **New examples**:
  - `examples/07-schema-evolution/` — schema migration pattern
  - `examples/08-custom-adapter/` — implementing custom storage adapters
- **CI workflows**:
  - `nightly.yml` — fuzz tests, benchmarks, sub-module matrix
  - `release.yml` — automated GitHub Release on tag push

### Changed

- README expanded with multi-backend storage, OLAP, cold store, and advanced
  features documentation
- `.gitignore` updated with compiled example binary names

[1.0.0]: https://github.com/abhipray-cpu/go-audit/releases/tag/v1.0.0

## [0.2.0] — 2026-02-14

### Added

- **Background mode**: Async version writes via bounded worker pool (`app.Pool`)
  - `PendingVersion` result type with `Wait(ctx)` for deferred error handling
  - Configurable `PoolWorkers` and `PoolQueueSize` with backpressure callback
- **Delta strategy**: Store only changed fields, with automatic fallback to full snapshot on diff failure
  - Delta validation: `Apply(prev, delta) == current` verified before persist
- **Hybrid strategy**: Full snapshot every N-th version, deltas in between
  - `Config.HybridSnapshotInterval` (default: 10)
- **Reconstruction engine**: `reconstruct.Reconstruct()` — rebuild entity state from delta chain
- **Compaction engine**: `reconstruct.Compactor` — bounds delta chains via synthetic snapshots
- **Write-Ahead Log (WAL)**:
  - `adapter/wal/noop.WAL` — zero-overhead default
  - `adapter/wal/file.WAL` — append-only file with CRC32 integrity and fsync
  - WAL integration: append before enqueue, ack after persist
  - `Config.WAL` and `Config.WALDir` convenience fields
- **LRU cache**: `adapter/cache.LRU` — thread-safe LRU cache implementing `CachePort`
  - Write-through on every version write
  - LRU eviction on capacity overflow
- **Subscriber fan-out**: `adapter/subscriber.Fanout` — concurrent dispatch with per-subscriber timeout and failure isolation
  - `adapter/subscriber.Log` — default structured log subscriber (wired automatically)
  - `Config.Subscriber` field with `LogSubscriber` as default
- **ClickHouse batch buffer**: `adapter/clickhouse.BatchBuffer` — async flush on size threshold or timer, retry with exponential backoff
  - `Config.OLAPBatchSize` and `Config.OLAPFlushInterval`
- **Storage router**: `app.StorageRouter` — sync OLTP + async OLAP routing, OLAP failure isolation
- **Advanced queries**:
  - `Auditor.Compare()` — diff two versions
  - `Auditor.FindChanges()` — field-level change history
  - `Auditor.FindByActor()` — filter versions by actor
  - `Auditor.GetBulkAtTime()` — multi-entity point-in-time query
- **Runtime guardrails**: `Config.MaxEntitySizeBytes` — reject oversized entities with `ErrEntityTooLarge`
- **OTEL plugin**: `plugin/otel` — separate sub-module implementing `InstrumenterPort`
  - 25 metric names defined (`audit.version.duration`, `audit.cache.hit_total`, etc.)
  - No OTEL dependency in core `go.mod`
- **HealthCheckPort**: `adapter/health.Checker` — backend liveness probes for K8s
- **MsgPack serializer**: `adapter/serializer/msgpack` — separate sub-module for compact binary serialization
- **HTTP metadata extractor**: `adapter/metadata.HTTPExtractor` — extracts actor, reason, correlation ID, traceparent from HTTP headers
- **gRPC metadata extractor**: `adapter/metadata.GRPCExtractor` — extracts audit metadata from gRPC metadata
- **New examples**:
  - `examples/04-background-mode/` — high-throughput async versioning
  - `examples/05-otel-observability/` — OTEL integration pattern
  - `examples/06-wal/` — WAL configuration for crash recovery

### Changed

- `TestGetAtTime` updated to verify actual point-in-time query (previously tested stub behavior)

[0.2.0]: https://github.com/abhipray-cpu/go-audit/releases/tag/v0.2.0

## [0.1.0] — 2026-02-14

### Added

- **Core API**: `audit.New()`, `Config` struct with sensible defaults
- **Version pipeline**: `Auditor.Version()` — clone → registry → sequence → diff → serialize → write
- **Transaction support**: `Auditor.VersionInTx()` — version within a caller-managed DB transaction
- **Query API**: `GetVersion()`, `GetLatest()`, `GetAtTime()`, `ListVersions()`
- **Context helpers**: `WithActor()`, `WithReason()`, `WithCorrelationID()`, `WithCustomMetadata()`
- **Metadata extraction**: Automatic context metadata on every version record
- **Intelligent diff engine**: Field-level change detection with tag annotations
  - Tags: `version:"id"`, `version:"tracked"`, `version:"ignore"`, `version:"normalized"`, `version:"unordered"`
  - Nested structs, embedded structs, maps, slices, pointers, `time.Time`
  - Normalization hooks and graceful degradation to full snapshot
  - Property-based tests and benchmarks
- **Entity registry**: Type-safe registration with field annotation parsing and strict mode
- **Adapters**:
  - JSON serializer (`adapter/serializer`)
  - Reflection-based deep cloner (`adapter/cloner`)
  - slog logger adapter (`adapter/logger`)
  - Context metadata extractor (`adapter/metadata`)
  - PostgreSQL schema + auto-migrate (`adapter/postgres`)
  - PostgreSQL writer — `Save()`, `SaveInTx()` (`adapter/postgres`)
  - PostgreSQL reader — all 4 query methods (`adapter/postgres`)
- **Testing utilities** (`testing/` package):
  - `InMemoryStore` — thread-safe in-memory writer + reader
  - `NewAuditor(t)` — zero-config test auditor
  - Noop adapters: `NoopWAL`, `NoopCache`, `NoopInstrumenter`, `NoopSubscriber`
  - Assertions: `AssertVersionCount`, `AssertFieldChanged`, `AssertActorIs`, `AssertLatestVersion`
  - Fixtures: `FixtureUser`, `FixtureAddress`, `SeedUserHistory()`
  - `TestInfra` — testcontainers helper for integration tests
- **Domain types**: `VersionRecord`, `Delta`, `FieldChange`, `Strategy`, `ActorType`, `VersionMetadata`
- **Typed errors**: `ErrStorage`, `ErrDiff`, `ErrSerialization`, `ErrValidation`, `ErrConfiguration`, `ErrNotFound`
- **Port interfaces**: 15 outbound ports, 3 inbound ports
- **CI**: GitHub Actions with Go 1.21/1.22 matrix, race detector, PostgreSQL integration
- **Examples**: basic usage, transaction support, testing helpers
- **License**: Apache 2.0

[0.1.0]: https://github.com/abhipray-cpu/go-audit/releases/tag/v0.1.0
