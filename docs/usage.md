# go-audit — Usage Guide

A comprehensive guide for integrating `go-audit` into your Go application.

---

## Table of Contents

1. [Installation](#installation)
2. [Quick Start](#quick-start)
3. [Entity Registration](#entity-registration)
4. [Creating Versions](#creating-versions)
5. [Context Metadata](#context-metadata)
6. [Querying Versions](#querying-versions)
7. [Storage Strategies](#storage-strategies)
8. [Storage Backends](#storage-backends)
9. [Transaction Support](#transaction-support)
10. [Hash Chain Integrity](#hash-chain-integrity)
11. [Schema Evolution](#schema-evolution)
12. [Write-Ahead Log](#write-ahead-log)
13. [Field Redaction](#field-redaction)
14. [Authorization Hooks](#authorization-hooks)
15. [Multi-Tenancy](#multi-tenancy)
16. [Cold Storage Tiering](#cold-storage-tiering)
17. [Observability](#observability)
18. [Testing](#testing)
19. [Configuration Reference](#configuration-reference)

---

## Installation

```bash
go get github.com/abhipray-cpu/go-audit
```

Requires **Go 1.21+**. Storage backends are separate modules — install only what you need:

```bash
# PostgreSQL (pgx/v5)
go get github.com/abhipray-cpu/go-audit/adapter/postgres

# MySQL
go get github.com/abhipray-cpu/go-audit/adapter/mysql

# ClickHouse (OLAP)
go get github.com/abhipray-cpu/go-audit/adapter/clickhouse

# S3 cold storage
go get github.com/abhipray-cpu/go-audit/adapter/coldstore/s3

# GCS cold storage
go get github.com/abhipray-cpu/go-audit/adapter/coldstore/gcs

# OTEL observability plugin
go get github.com/abhipray-cpu/go-audit/plugin/otel
```

---

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    audit "github.com/abhipray-cpu/go-audit"
    "github.com/abhipray-cpu/go-audit/domain"
    audittest "github.com/abhipray-cpu/go-audit/testing"
)

type User struct {
    ID    string `version:"id"`
    Name  string `version:"tracked"`
    Email string `version:"tracked"`
}

func main() {
    store := audittest.NewInMemoryStore()

    auditor, err := audit.New(audit.Config{
        Writer: store,
        Reader: store,
    })
    if err != nil {
        log.Fatal(err)
    }
    defer auditor.Shutdown(context.Background())

    auditor.Register(&User{})

    ctx := audit.WithActor(context.Background(), "user-123", domain.ActorHuman)
    ctx = audit.WithReason(ctx, "profile update")

    user := &User{ID: "u1", Name: "Alice", Email: "alice@example.com"}
    pending, err := auditor.Version(ctx, user)
    if err != nil {
        log.Fatal(err)
    }

    result, err := pending.Wait(ctx)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Created version %d for %s/%s\n",
        result.Version, result.EntityType, result.EntityID)
}
```

---

## Entity Registration

Annotate struct fields with `version` tags to control how they're tracked:

```go
type Order struct {
    ID        string    `version:"id"`       // Required: unique entity identifier
    Status    string    `version:"tracked"`  // Tracked: included in diffs
    Amount    float64   `version:"tracked"`
    Notes     string    `version:"tracked"`
    Secret    string    `version:"redacted"` // Redacted: stored as "[REDACTED]"
    CacheKey  string    `version:"ignore"`   // Ignored: excluded entirely
}
```

### Tag Reference

| Tag | Behavior |
|-----|----------|
| `version:"id"` | Marks the entity's unique identifier field. Exactly one required per struct. |
| `version:"tracked"` | Field is included in version snapshots and diffs. |
| `version:"redacted"` | Field is stored as `[REDACTED]` — useful for PII/GDPR compliance. |
| `version:"ignore"` | Field is excluded from versioning entirely. |

### Registration

Register entity types once at startup, before serving traffic:

```go
if err := auditor.Register(&User{}); err != nil {
    log.Fatal(err) // Invalid struct — missing version:"id", duplicate type, etc.
}
```

> **Note**: `Register()` is NOT safe to call concurrently with `Version()`.
> Register all types during initialization.

---

## Creating Versions

### Background Mode (Default)

Returns in ~1ms. The actual persist happens asynchronously in the worker pool.

```go
pending, err := auditor.Version(ctx, user)
if err != nil {
    return err
}

// Optional: wait for the background persist to complete.
record, err := pending.Wait(ctx)
```

### Sync Mode

Blocks until the version is fully persisted. Use when you need the result immediately.

```go
import "github.com/abhipray-cpu/go-audit/domain/port"

pending, err := auditor.Version(ctx, user, port.WithSync())
record, err := pending.Wait(ctx)
// record is already available — Wait returns immediately.
```

---

## Context Metadata

Attach audit metadata to the context. It's automatically captured with every version.

```go
// Who made the change
ctx = audit.WithActor(ctx, "user-123", domain.ActorHuman)

// Why
ctx = audit.WithReason(ctx, "quarterly address update")

// Cross-service tracing
ctx = audit.WithCorrelationID(ctx, requestID)

// Arbitrary key-value pairs
ctx = audit.WithCustomMetadata(ctx, "ticket", "JIRA-1234")
ctx = audit.WithCustomMetadata(ctx, "source", "admin-panel")
```

### Actor Types

```go
domain.ActorHuman  // Human user
domain.ActorSystem // Automated system process
domain.ActorAPI    // External API caller
```

> **Safety**: All `With*` functions sanitize null bytes (`\x00`) from input
> strings to prevent PostgreSQL encoding errors.

---

## Querying Versions

```go
// Get a specific version
record, err := auditor.GetVersion(ctx, "user", "u1", 3)

// Get the latest version
latest, err := auditor.GetLatest(ctx, "user", "u1")

// Get entity state at a point in time
snapshot, err := auditor.GetAtTime(ctx, "user", "u1", time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))

// List all versions (with pagination)
versions, err := auditor.ListVersions(ctx, "user", "u1",
    port.WithLimit(20),
    port.WithOffset(0),
)

// Compare two versions — get the diff
delta, err := auditor.Compare(ctx, "user", "u1", 1, 5)

// Find all changes to a specific field
changes, err := auditor.FindChanges(ctx, "user", "u1", "Email")

// Find all versions by a specific actor
records, err := auditor.FindByActor(ctx, "admin-1")

// Bulk point-in-time query
snapshots, err := auditor.GetBulkAtTime(ctx, "user",
    []string{"u1", "u2", "u3"},
    time.Now().Add(-24*time.Hour),
)
```

---

## Storage Strategies

Control how entity data is stored per version:

```go
auditor, _ := audit.New(audit.Config{
    Writer:   store,
    Reader:   store,
    Strategy: domain.StrategyHybrid,
    HybridSnapshotInterval: 10, // Full snapshot every 10th version
})
```

| Strategy | Storage | Read Cost | Best For |
|----------|---------|-----------|----------|
| `StrategyFull` (default) | Complete snapshot every version | O(1) — direct read | Low-volume, simplicity |
| `StrategyDelta` | Only changed fields | O(n) — reconstruct from base + deltas | High-volume, storage-constrained |
| `StrategyHybrid` | Full every N versions, deltas between | O(N) max — bounded reconstruction | Production workloads |

---

## Storage Backends

### PostgreSQL (Primary OLTP)

```go
import "github.com/abhipray-cpu/go-audit/adapter/postgres"

pool, _ := pgxpool.New(ctx, "postgres://user:pass@localhost:5432/mydb")

writer, reader := postgres.NewWriter(pool), postgres.NewReader(pool)
if err := postgres.Migrate(ctx, pool); err != nil {
    log.Fatal(err)
}

auditor, _ := audit.New(audit.Config{
    Writer: writer,
    Reader: reader,
})
```

### MySQL

```go
import "github.com/abhipray-cpu/go-audit/adapter/mysql"

db, _ := sql.Open("mysql", "user:pass@tcp(localhost:3306)/mydb")

writer, reader := mysql.NewWriter(db), mysql.NewReader(db)
if err := mysql.Migrate(ctx, db); err != nil {
    log.Fatal(err)
}

auditor, _ := audit.New(audit.Config{
    Writer: writer,
    Reader: reader,
})
```

### ClickHouse (OLAP Analytics)

```go
import "github.com/abhipray-cpu/go-audit/adapter/clickhouse"

chDB, _ := sql.Open("clickhouse", "clickhouse://localhost:9000/default")

olapAdapter := clickhouse.NewAdapter(chDB)

auditor, _ := audit.New(audit.Config{
    Writer:            pgWriter,
    Reader:            pgReader,
    OLAPBatchSize:     1000,
    OLAPFlushInterval: 5 * time.Second,
})
```

### Custom Adapters

Implement `port.VersionWriterPort` and `port.VersionReaderPort`:

```go
type VersionWriterPort interface {
    Save(ctx context.Context, record domain.VersionRecord) error
    Update(ctx context.Context, record domain.VersionRecord) error
}

type VersionReaderPort interface {
    GetVersion(ctx context.Context, entityType, entityID string, version int64) (domain.VersionRecord, error)
    GetLatest(ctx context.Context, entityType, entityID string) (domain.VersionRecord, error)
    ListVersions(ctx context.Context, entityType, entityID string, opts port.ListOptions) ([]domain.VersionRecord, error)
    // ... additional query methods
}
```

See [`examples/08-custom-adapter/`](../examples/08-custom-adapter/) for a complete walkthrough.

---

## Transaction Support

Ensure the version record is committed atomically with your business write:

```go
tx, _ := pool.Begin(ctx)
defer tx.Rollback(ctx)

// Your business logic
tx.Exec(ctx, "UPDATE users SET email = $1 WHERE id = $2", newEmail, userID)

// Version is part of the same transaction
record, err := auditor.VersionInTx(ctx, tx, updatedUser)
if err != nil {
    return err
}

tx.Commit(ctx) // Both user update and version are atomic
```

> `VersionInTx` runs in sync mode — it bypasses the worker pool entirely.

---

## Hash Chain Integrity

Enable tamper-evident audit trails with SHA-256 hash chains:

```go
import "github.com/abhipray-cpu/go-audit/adapter/hash"

auditor, _ := audit.New(audit.Config{
    Writer:          store,
    Reader:          store,
    EnableHashChain: true,
    Hasher:          hash.NewSHA256(),
})
```

Verify the chain hasn't been tampered with:

```go
err := auditor.VerifyIntegrity(ctx, "order", "order-123")
if err != nil {
    // Chain is broken — records may have been tampered with
    log.Error("integrity violation", "error", err)
}
```

---

## Schema Evolution

When your entity structs change across releases, register migrations so old versions remain readable:

```go
auditor.RegisterMigration("user", 1, 2, func(ctx context.Context, data []byte) ([]byte, error) {
    // Transform v1 serialized data to v2 format.
    var old map[string]interface{}
    json.Unmarshal(data, &old)
    old["full_name"] = old["first_name"].(string) + " " + old["last_name"].(string)
    delete(old, "first_name")
    delete(old, "last_name")
    return json.Marshal(old)
})
```

Migrations are **lazy** — applied on read, not backfilled. They're **forward-only** and **verified at registration** (gaps like v1→v3 without v2 are rejected).

---

## Write-Ahead Log

For crash durability, enable a WAL so in-flight versions survive process restarts:

```go
// File-based WAL — VMs, K8s with PersistentVolumes
auditor, _ := audit.New(audit.Config{
    Writer: store,
    Reader: store,
    WALDir: "/var/lib/go-audit/wal",
})

// Database-backed WAL — serverless, ephemeral containers
import waldb "github.com/abhipray-cpu/go-audit/adapter/wal/db"

dbWAL, _ := waldb.New(db)
auditor, _ := audit.New(audit.Config{
    Writer: store,
    Reader: store,
    WAL:    dbWAL,
})
```

| WAL Type | Latency Overhead | Best For |
|----------|-----------------|----------|
| None (default) | +0ms | Most services — process-durable only |
| File WAL | +0.3–0.5ms | VMs, K8s with persistent volumes |
| DB WAL | +3–8ms | Serverless, multi-pod, no local disk |

---

## Field Redaction

Mark sensitive fields for automatic redaction in stored versions:

```go
type Patient struct {
    ID     string `version:"id"`
    Name   string `version:"tracked"`
    SSN    string `version:"redacted"` // Stored as "[REDACTED]"
    DOB    string `version:"redacted"`
}
```

You can also redact fields after the fact:

```go
err := auditor.Redact(ctx, "patient", "p-123",
    port.RedactFields("SSN", "DOB"),
)
```

---

## Authorization Hooks

Add before-read and before-write lifecycle callbacks:

```go
import "github.com/abhipray-cpu/go-audit/domain/hooks"

registry := hooks.New()
registry.BeforeWrite("order", func(ctx context.Context, entityType, entityID string) error {
    if !isAdmin(ctx) {
        return fmt.Errorf("unauthorized: only admins can modify order audit trails")
    }
    return nil
})

auditor, _ := audit.New(audit.Config{
    Writer: store,
    Reader: store,
    Hooks:  registry,
})
```

---

## Multi-Tenancy

Scope audit data by tenant using context:

```go
ctx = audit.WithTenant(ctx, "tenant-abc")

// All versions created with this context are scoped to "tenant-abc"
auditor.Version(ctx, entity)

// Queries automatically filter by tenant
auditor.ListVersions(ctx, "user", "u1")
```

---

## Cold Storage Tiering

Archive old versions to S3 or GCS:

```go
import "github.com/abhipray-cpu/go-audit/adapter/coldstore/s3"

coldStore := s3.New(s3.Config{
    Bucket: "my-audit-archive",
    Prefix: "audit/",
    Region: "us-east-1",
})

auditor, _ := audit.New(audit.Config{
    Writer:        pgWriter,
    Reader:        pgReader,
    RetentionDays: map[string]int{
        "user":  365,  // Keep user versions for 1 year in hot storage
        "order": 90,   // Keep order versions for 90 days
    },
})
```

---

## Observability

Install the OTEL plugin (separate module — zero impact on core):

```go
import "github.com/abhipray-cpu/go-audit/plugin/otel"

// The OTEL adapter implements InstrumenterPort
// Exposes 24+ metrics including:
//   audit.version.created       (counter)
//   audit.version.duration      (histogram)
//   audit.version.queue_depth   (gauge)
//   audit.diff.computed         (counter)
//   audit.store.write.duration  (histogram)
//   audit.store.cache.hit/miss  (counter)
```

### Health Check

```go
status := auditor.HealthCheck(ctx)
// Checks: OLTP ping, OLAP ping (if configured), Cold (if configured)
if !status.Healthy {
    // One or more backends are unhealthy
}
```

---

## Testing

The library ships zero-dependency test helpers:

```go
import audittest "github.com/abhipray-cpu/go-audit/testing"

func TestMyService(t *testing.T) {
    auditor, store := audittest.NewAuditor(t)
    auditor.Register(&User{})

    // ... exercise your code ...

    audittest.AssertVersionCount(t, store, "user", "u1", 3)
    audittest.AssertLatestVersion(t, store, "user", "u1", 3)
}
```

### Time Mocking

```go
clock := audittest.NewMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
auditor := audittest.NewAuditor(t, audittest.WithClock(clock))
clock.Advance(24 * time.Hour) // Simulate time passing
```

---

## Configuration Reference

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Writer` | `port.VersionWriterPort` | *required* | Primary storage writer |
| `Reader` | `port.VersionReaderPort` | *required* | Primary storage reader |
| `Logger` | `port.LoggerPort` | slog default | Structured logger |
| `Serializer` | `port.SerializerPort` | JSON | Entity serialization format |
| `Cloner` | `port.ClonerPort` | Reflect | Deep-copy strategy |
| `Differ` | `port.DifferPort` | StructDiffer | Diff engine |
| `MetadataExtractor` | `port.MetadataExtractorPort` | ContextExtractor | Metadata extraction |
| `Strategy` | `domain.Strategy` | `StrategyFull` | Storage strategy |
| `HybridSnapshotInterval` | `int` | 10 | Full snapshot frequency in hybrid mode |
| `PoolWorkers` | `int` | `NumCPU() * 2` | Background worker goroutines |
| `PoolQueueSize` | `int` | 10,000 | Work channel capacity |
| `WAL` | `port.WALPort` | nil (disabled) | Write-ahead log adapter |
| `WALDir` | `string` | "" | Convenience: auto-create FileWAL at path |
| `EnableHashChain` | `bool` | false | Enable SHA-256 tamper-evident chain |
| `Hasher` | `port.HashComputerPort` | SHA-256 | Hash algorithm (when chain enabled) |
| `MaxEntitySizeBytes` | `int64` | 0 (disabled) | Runtime entity size limit |
| `MaxMetadataSizeBytes` | `int64` | 0 (disabled) | Runtime metadata size limit |
| `Subscriber` | `port.SubscriberPort` | LogSubscriber | Post-persist notification |
| `OLAPBatchSize` | `int` | 1000 | OLAP flush batch threshold |
| `OLAPFlushInterval` | `time.Duration` | 5s | OLAP max flush interval |
| `RetentionDays` | `map[string]int` | nil | Per-entity-type retention (days) |
| `Hooks` | `port.HooksPort` | nil | Authorization lifecycle hooks |
| `Clock` | `port.ClockPort` | System clock | Time source (swap for testing) |
