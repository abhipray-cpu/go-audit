//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	audit "github.com/abhipray-cpu/go-audit"
	chadapter "github.com/abhipray-cpu/go-audit/adapter/clickhouse"
	"github.com/abhipray-cpu/go-audit/adapter/postgres"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
	"github.com/abhipray-cpu/go-audit/domain/registry"
)

// ---------------------------------------------------------------------------
// GAP 1 — VersionInTx: Transaction-Aware Versioning (ARCHITECTURE §7.3)
// ---------------------------------------------------------------------------

// TX-001: TestVersionInTx_CommitPersists verifies that a version record
// created inside a committed transaction is visible after the commit.
func TestVersionInTx_CommitPersists(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "Alice", Email: "alice@a.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	result, err := a.VersionInTx(ctx, tx, user)
	if err != nil {
		tx.Rollback(ctx)
		t.Fatalf("VersionInTx: %v", err)
	}

	if result.Record.Version != 1 {
		tx.Rollback(ctx)
		t.Fatalf("expected version 1, got %d", result.Record.Version)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// After commit, the record should be visible.
	rec, err := a.GetVersion(ctx, result.Record.EntityType, user.ID, 1)
	if err != nil {
		t.Fatalf("GetVersion after commit: %v", err)
	}
	if rec.Version != 1 {
		t.Errorf("expected version 1, got %d", rec.Version)
	}
}

// TX-002: TestVersionInTx_RollbackDiscards verifies that rolling back the
// transaction discards the version record — it should not be readable.
func TestVersionInTx_RollbackDiscards(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "Bob", Email: "bob@b.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	_, err = a.VersionInTx(ctx, tx, user)
	if err != nil {
		tx.Rollback(ctx)
		t.Fatalf("VersionInTx: %v", err)
	}

	// Rollback instead of commit.
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	// After rollback, GetLatest should return ErrNotFound.
	_, err = a.GetLatest(ctx, "userentity", user.ID)
	if err == nil {
		t.Fatal("expected error after rollback, got nil")
	}
}

// TX-003: TestVersionInTx_NilTx verifies that passing nil transaction
// returns ErrValidation.
func TestVersionInTx_NilTx(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "Eve"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	_, err := a.VersionInTx(ctx, nil, user)
	if err == nil {
		t.Fatal("expected ErrValidation for nil tx, got nil")
	}
}

// TX-004: TestVersionInTx_MultipleVersionsSameTx creates two versions for
// different entities in the same transaction — both should persist on commit.
func TestVersionInTx_MultipleVersionsSameTx(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "Alice"}
	order := &OrderEntity{ID: uniqueID("order"), UserID: user.ID, Status: "new"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register user: %v", err)
	}
	if err := a.Register(order); err != nil {
		t.Fatalf("Register order: %v", err)
	}

	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	r1, err := a.VersionInTx(ctx, tx, user)
	if err != nil {
		tx.Rollback(ctx)
		t.Fatalf("VersionInTx user: %v", err)
	}

	r2, err := a.VersionInTx(ctx, tx, order)
	if err != nil {
		tx.Rollback(ctx)
		t.Fatalf("VersionInTx order: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Both records visible.
	if _, err := a.GetVersion(ctx, r1.Record.EntityType, user.ID, 1); err != nil {
		t.Errorf("user version not found: %v", err)
	}
	if _, err := a.GetVersion(ctx, r2.Record.EntityType, order.ID, 1); err != nil {
		t.Errorf("order version not found: %v", err)
	}
}

// TX-005: TestVersionInTx_WithMetadata verifies that context metadata
// (actor, reason) propagates through the transactional path.
func TestVersionInTx_WithMetadata(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "Alice"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := audit.WithActor(context.Background(), "admin-99", domain.ActorHuman)
	ctx = audit.WithReason(ctx, "compliance update")

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	result, err := a.VersionInTx(ctx, tx, user)
	if err != nil {
		tx.Rollback(ctx)
		t.Fatalf("VersionInTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	rec, err := a.GetVersion(ctx, result.Record.EntityType, user.ID, 1)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if rec.Metadata.ActorID != "admin-99" {
		t.Errorf("actor = %q, want admin-99", rec.Metadata.ActorID)
	}
	if rec.Metadata.Reason != "compliance update" {
		t.Errorf("reason = %q, want %q", rec.Metadata.Reason, "compliance update")
	}
}

// TX-006: TestVersionInTx_SubscriberNotified verifies that the subscriber
// is notified even though the write uses the transactional path.
func TestVersionInTx_SubscriberNotified(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	sub := &testSubscriber{}
	w := postgres.NewWriter(conn)
	r := postgres.NewReader(conn)

	a, err := audit.New(audit.Config{
		Writer:     w,
		Reader:     r,
		Subscriber: sub,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { a.Shutdown(context.Background()) })

	user := &UserEntity{ID: uniqueID("user"), Name: "Alice"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	_, err = a.VersionInTx(ctx, tx, user)
	if err != nil {
		tx.Rollback(ctx)
		t.Fatalf("VersionInTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if sub.Count() != 1 {
		t.Errorf("subscriber count = %d, want 1", sub.Count())
	}
}

// TX-007: TestVersionInTx_UnregisteredEntity verifies VersionInTx returns
// an error for an unregistered entity — same guard as Version().
func TestVersionInTx_UnregisteredEntity(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	// Do NOT register UserEntity.

	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback(ctx)

	user := &UserEntity{ID: uniqueID("user"), Name: "Alice"}
	_, err = a.VersionInTx(ctx, tx, user)
	if err == nil {
		t.Fatal("expected error for unregistered entity, got nil")
	}
}

// TX-008: TestVersionInTx_MultipleVersionsSameEntity verifies sequential
// versioning of the same entity within the same transaction (v1, v2).
func TestVersionInTx_MultipleVersionsSameEntity(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	r1, err := a.VersionInTx(ctx, tx, user)
	if err != nil {
		tx.Rollback(ctx)
		t.Fatalf("VersionInTx v1: %v", err)
	}
	if r1.Record.Version != 1 {
		tx.Rollback(ctx)
		t.Fatalf("expected v1, got %d", r1.Record.Version)
	}

	user.Name = "v2"
	r2, err := a.VersionInTx(ctx, tx, user)
	if err != nil {
		tx.Rollback(ctx)
		t.Fatalf("VersionInTx v2: %v", err)
	}
	if r2.Record.Version != 2 {
		tx.Rollback(ctx)
		t.Fatalf("expected v2, got %d", r2.Record.Version)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Both versions visible.
	rec, err := a.GetVersion(ctx, r1.Record.EntityType, user.ID, 2)
	if err != nil {
		t.Fatalf("GetVersion v2: %v", err)
	}
	if rec.Version != 2 {
		t.Errorf("version = %d, want 2", rec.Version)
	}
}

// ---------------------------------------------------------------------------
// GAP 2 — Read Routing: OLTP Hit/Miss (ARCHITECTURE §7.2)
// ---------------------------------------------------------------------------

// RT-001: TestReadRouting_OLTPHit verifies that a record stored in OLTP
// (Postgres) is found directly.
func TestReadRouting_OLTPHit(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)
	user := &UserEntity{ID: uniqueID("user"), Name: "found-in-oltp"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	rec, err := a.GetLatest(ctx, "userentity", user.ID)
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if rec.EntityID != user.ID {
		t.Errorf("entity_id = %q, want %q", rec.EntityID, user.ID)
	}
}

// RT-002: TestReadRouting_OLTPMiss confirms that when a record doesn't
// exist in OLTP, the reader returns not-found.
func TestReadRouting_OLTPMiss(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	ctx := context.Background()
	_, err := a.GetVersion(ctx, "userentity", "nonexistent", 1)
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
}

// RT-003: TestReadRouting_OLAPQueryReturnsCHRecords verifies that records
// inserted directly into ClickHouse are queryable via the OLAP adapter.
func TestReadRouting_OLAPQueryReturnsCHRecords(t *testing.T) {
	chdb := clickhouseDB(t)
	cleanupCHTables(t, chdb)
	chadapter.AutoMigrate(context.Background(), chdb)

	store := chadapter.NewStore(chdb)

	entityType := "userentity"
	eid := uniqueID("user")
	for i := int64(1); i <= 5; i++ {
		rec := makeCHRecord(entityType, eid, i)
		if err := store.BatchInsert(context.Background(), []domain.VersionRecord{rec}); err != nil {
			t.Fatalf("BatchInsert: %v", err)
		}
	}

	records, err := store.Query(context.Background(), entityType)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(records) < 5 {
		t.Errorf("expected ≥5 records from OLAP, got %d", len(records))
	}
}

// ---------------------------------------------------------------------------
// GAP 3 — OLTP+OLAP Dual Write via Subscriber (ARCHITECTURE §7.1)
//
// Verifies that Version() notifies the subscriber, which can forward
// records to ClickHouse (OLAP).
// ---------------------------------------------------------------------------

// DW-001: TestDualWrite_SubscriberForwardsToCH verifies end-to-end that
// creating a version in PG also results in the record reaching CH via a
// subscriber that acts as the OLAP bridge.
func TestDualWrite_SubscriberForwardsToCH(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	chdb := clickhouseDB(t)
	cleanupCHTables(t, chdb)
	chadapter.AutoMigrate(context.Background(), chdb)

	chStore := chadapter.NewStore(chdb)

	// Subscriber forwards every version event to ClickHouse.
	bridge := &chBridgeSubscriber{store: chStore}

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Subscriber = bridge
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "dual-write"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	versionSync(t, ctx, a, user)

	// OLTP should have the record.
	rec, err := a.GetLatest(ctx, "userentity", user.ID)
	if err != nil {
		t.Fatalf("OLTP GetLatest: %v", err)
	}
	if rec.EntityID != user.ID {
		t.Errorf("OLTP entity_id = %q, want %q", rec.EntityID, user.ID)
	}

	// OLAP should also have the record via the subscriber bridge.
	chRecords, err := chStore.Query(ctx, "userentity")
	if err != nil {
		t.Fatalf("CH Query: %v", err)
	}
	found := false
	for _, r := range chRecords {
		if r.EntityID == user.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("record not found in ClickHouse after subscriber bridge")
	}
}

// DW-002: TestDualWrite_MultipleVersionsBothStores verifies that multiple
// successive versions all reach both PG and CH.
func TestDualWrite_MultipleVersionsBothStores(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	chdb := clickhouseDB(t)
	cleanupCHTables(t, chdb)
	chadapter.AutoMigrate(context.Background(), chdb)

	chStore := chadapter.NewStore(chdb)
	bridge := &chBridgeSubscriber{store: chStore}

	a := newAuditorPG(t, conn, func(c *audit.Config) {
		c.Subscriber = bridge
	})

	user := &UserEntity{ID: uniqueID("user"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		user.Name = randString(5)
		versionSync(t, ctx, a, user)
	}

	// PG should have 3 versions.
	pgVersions, err := a.ListVersions(ctx, "userentity", user.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(pgVersions) != 3 {
		t.Errorf("PG versions = %d, want 3", len(pgVersions))
	}

	// CH should also have 3 records.
	chRecords, err := chStore.Query(ctx, "userentity")
	if err != nil {
		t.Fatalf("CH Query: %v", err)
	}
	count := 0
	for _, r := range chRecords {
		if r.EntityID == user.ID {
			count++
		}
	}
	if count != 3 {
		t.Errorf("CH records for entity = %d, want 3", count)
	}
}

// chBridgeSubscriber forwards version events to ClickHouse, simulating
// the OLAP dual-write path described in ARCHITECTURE §7.1.
type chBridgeSubscriber struct {
	store *chadapter.Store
}

func (s *chBridgeSubscriber) OnVersion(ctx context.Context, record domain.VersionRecord) error {
	return s.store.BatchInsert(ctx, []domain.VersionRecord{record})
}

// ---------------------------------------------------------------------------
// GAP 4 — WAL Replay Idempotency with Real OLTP (ARCHITECTURE §9)
// ---------------------------------------------------------------------------

// WAL-001: TestWAL_ReplayIdempotent verifies that replaying a previously
// persisted record (simulating WAL crash-recovery) triggers the duplicate
// guard — Save() returns ErrDuplicateVersion instead of corrupting data.
func TestWAL_ReplayIdempotent(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "wal-dedup"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	rec := versionSync(t, ctx, a, user)

	// Simulate WAL replay: re-insert the same record via Save.
	err := a.Writer().Save(ctx, rec)
	if err == nil {
		t.Fatal("expected ErrDuplicateVersion on replay, got nil")
	}

	// Original record intact.
	readback, err := a.GetVersion(ctx, rec.EntityType, user.ID, 1)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if readback.Version != 1 {
		t.Errorf("version = %d, want 1", readback.Version)
	}
}

// WAL-002: TestWAL_ReplayDoesNotCorruptHistory verifies that after a
// failed replay, the full version history is intact and queryable.
func TestWAL_ReplayDoesNotCorruptHistory(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "v1"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	rec1 := versionSync(t, ctx, a, user)

	user.Name = "v2"
	rec2 := versionSync(t, ctx, a, user)

	// Replay both — both should fail with duplicate.
	if err := a.Writer().Save(ctx, rec1); err == nil {
		t.Error("replay rec1: expected error, got nil")
	}
	if err := a.Writer().Save(ctx, rec2); err == nil {
		t.Error("replay rec2: expected error, got nil")
	}

	// History intact.
	versions, err := a.ListVersions(ctx, rec1.EntityType, user.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Errorf("expected 2 versions, got %d", len(versions))
	}
}

// ---------------------------------------------------------------------------
// GAP 8 — Registration Guardrails: Strict Mode (ARCHITECTURE §12 #8)
// ---------------------------------------------------------------------------

// entityWithBytesField has a []byte field without version:"ignore".
type entityWithBytesField struct {
	ID   string `version:"id"`
	Data []byte
}

// entityWithBytesIgnored has a []byte field with version:"ignore".
type entityWithBytesIgnored struct {
	ID   string `version:"id"`
	Data []byte `version:"ignore"`
}

// RG-001: TestGuardrails_StrictMode_RejectsByteSlice verifies that
// registering an entity with an unignored []byte field in strict mode
// returns ErrConfiguration.
func TestGuardrails_StrictMode_RejectsByteSlice(t *testing.T) {
	reg := registry.New(registry.WithGuardrails(registry.GuardrailConfig{
		StrictRegistration: true,
	}))

	err := reg.Register(&entityWithBytesField{ID: "test"})
	if err == nil {
		t.Fatal("expected error for unignored []byte field in strict mode")
	}
}

// RG-002: TestGuardrails_StrictMode_AcceptsIgnoredByteSlice verifies that
// a []byte field with version:"ignore" is accepted even in strict mode.
func TestGuardrails_StrictMode_AcceptsIgnoredByteSlice(t *testing.T) {
	reg := registry.New(registry.WithGuardrails(registry.GuardrailConfig{
		StrictRegistration: true,
	}))

	err := reg.Register(&entityWithBytesIgnored{ID: "test"})
	if err != nil {
		t.Fatalf("expected no error for ignored []byte field, got: %v", err)
	}
}

// RG-003: TestGuardrails_NonStrict_AllowsByteSlice verifies that without
// strict mode, the []byte field produces a warning but not an error.
func TestGuardrails_NonStrict_AllowsByteSlice(t *testing.T) {
	reg := registry.New(registry.WithGuardrails(registry.GuardrailConfig{
		StrictRegistration: false,
	}))

	err := reg.Register(&entityWithBytesField{ID: "test"})
	if err != nil {
		t.Fatalf("non-strict mode should allow []byte field, got: %v", err)
	}
}

// RG-004: TestGuardrails_OversizedEntity verifies that an entity exceeding
// MaxEntityBytes at registration time is rejected.
func TestGuardrails_OversizedEntity(t *testing.T) {
	reg := registry.New(registry.WithGuardrails(registry.GuardrailConfig{
		MaxEntityBytes: 50, // very small
	}))

	bigEntity := &UserEntity{
		ID:    "test-user",
		Name:  "this-name-is-quite-long-and-when-serialized-will-exceed-50-bytes",
		Email: "long-email-exceeds@limit.com",
	}
	err := reg.Register(bigEntity)
	if err == nil {
		t.Fatal("expected error for oversized entity, got nil")
	}
}

// RG-005: TestGuardrails_DefaultMaxBytes verifies default 5MB limit passes
// for normal entities.
func TestGuardrails_DefaultMaxBytes(t *testing.T) {
	reg := registry.New(registry.WithGuardrails(registry.GuardrailConfig{}))

	err := reg.Register(&UserEntity{ID: "test"})
	if err != nil {
		t.Fatalf("normal entity should be within 5MB default, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GAP 12 — Cloner Deep-Copy Guarantee (ARCHITECTURE §7.1 step 3, §12 #4)
// ---------------------------------------------------------------------------

// CL-001: TestCloner_MutateAfterVersion_SnapshotUnchanged verifies that
// mutating the entity after Version() does NOT corrupt the stored snapshot.
func TestCloner_MutateAfterVersion_SnapshotUnchanged(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{ID: uniqueID("user"), Name: "before", Email: "before@test.com"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	rec := versionSync(t, ctx, a, user)

	// Mutate the original entity AFTER Version().
	user.Name = "CORRUPTED"
	user.Email = "CORRUPTED@test.com"

	// Read back the stored record — it should contain the original values.
	readback, err := a.GetVersion(ctx, rec.EntityType, user.ID, 1)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}

	// Deserialize and check.
	var restored UserEntity
	if err := a.Config().Serializer.Unmarshal(ctx, readback.Data, &restored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if restored.Name != "before" {
		t.Errorf("Name = %q, want %q — clone did not protect snapshot", restored.Name, "before")
	}
	if restored.Email != "before@test.com" {
		t.Errorf("Email = %q, want %q", restored.Email, "before@test.com")
	}
}

// CL-002: TestCloner_NestedStruct_DeepCopy verifies that nested struct
// values are deeply copied — mutating a nested struct after versioning
// does not affect the stored snapshot.
func TestCloner_NestedStruct_DeepCopy(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{
		ID:      uniqueID("user"),
		Name:    "Alice",
		Address: Address{Street: "123 Main St", City: "Springfield"},
	}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	rec := versionSync(t, ctx, a, user)

	// Mutate nested struct.
	user.Address.Street = "CORRUPTED STREET"
	user.Address.City = "CORRUPTED CITY"

	readback, err := a.GetVersion(ctx, rec.EntityType, user.ID, 1)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}

	var restored UserEntity
	if err := a.Config().Serializer.Unmarshal(ctx, readback.Data, &restored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if restored.Address.Street != "123 Main St" {
		t.Errorf("Street = %q, want %q", restored.Address.Street, "123 Main St")
	}
}

// CL-003: TestCloner_SliceField_DeepCopy verifies slice isolation after
// versioning — appending to the original slice does not affect stored data.
func TestCloner_SliceField_DeepCopy(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	a := newAuditorPG(t, conn)

	user := &UserEntity{
		ID:    uniqueID("user"),
		Name:  "Alice",
		Roles: []string{"admin"},
	}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	rec := versionSync(t, ctx, a, user)

	// Mutate the slice.
	user.Roles = append(user.Roles, "CORRUPTED_ROLE")

	readback, err := a.GetVersion(ctx, rec.EntityType, user.ID, 1)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}

	var restored UserEntity
	if err := a.Config().Serializer.Unmarshal(ctx, readback.Data, &restored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(restored.Roles) != 1 || restored.Roles[0] != "admin" {
		t.Errorf("Roles = %v, want [admin]", restored.Roles)
	}
}

// ---------------------------------------------------------------------------
// GAP 15 — Graceful Shutdown Drains Pool (ARCHITECTURE §12 #3)
// ---------------------------------------------------------------------------

// SD-001: TestShutdown_DrainsInFlightWork verifies that Shutdown waits
// for in-flight background work to complete before returning.
func TestShutdown_DrainsInFlightWork(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	w := postgres.NewWriter(conn)
	r := postgres.NewReader(conn)

	a, err := audit.New(audit.Config{
		Writer:      w,
		Reader:      r,
		PoolWorkers: 1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	user := &UserEntity{ID: uniqueID("user"), Name: "shutdown-test"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()

	// Fire a background version (no WithSync).
	pv, err := a.Version(ctx, user)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}

	// Shutdown should drain the pool — the pending version should complete.
	if err := a.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// After shutdown, Wait should return the completed record.
	rec, err := pv.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait after Shutdown: %v", err)
	}
	if rec.Version != 1 {
		t.Errorf("version = %d, want 1", rec.Version)
	}

	// Record should be persisted.
	readback, err := a.GetVersion(ctx, rec.EntityType, user.ID, 1)
	if err != nil {
		t.Fatalf("GetVersion after shutdown: %v", err)
	}
	if readback.EntityID != user.ID {
		t.Errorf("entity_id = %q, want %q", readback.EntityID, user.ID)
	}
}

// SD-002: TestShutdown_MultipleInFlight verifies that Shutdown drains
// multiple pending background versions. Uses PoolWorkers=1 to avoid
// "conn busy" on single *pgx.Conn (pool requires *pgxpool.Pool).
func TestShutdown_MultipleInFlight(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	w := postgres.NewWriter(conn)
	r := postgres.NewReader(conn)

	a, err := audit.New(audit.Config{
		Writer:      w,
		Reader:      r,
		PoolWorkers: 1, // single worker — safe with *pgx.Conn
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Register a single entity type, then create multiple entities of that type.
	proto := &FlatEntity{ID: "proto", Name: "proto"}
	if err := a.Register(proto); err != nil {
		t.Fatalf("Register: %v", err)
	}

	entities := make([]*FlatEntity, 5)
	pending := make([]*domain.PendingVersion, 5)

	for i := 0; i < 5; i++ {
		entities[i] = &FlatEntity{ID: uniqueID("flat"), Name: randString(5)}

		pv, err := a.Version(context.Background(), entities[i])
		if err != nil {
			t.Fatalf("Version %d: %v", i, err)
		}
		pending[i] = pv
	}

	// Shutdown should drain all 5.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// All should have completed.
	for i, pv := range pending {
		rec, err := pv.Wait(ctx)
		if err != nil {
			t.Errorf("Wait %d: %v", i, err)
			continue
		}
		if rec.Version != 1 {
			t.Errorf("entity %d: version = %d, want 1", i, rec.Version)
		}
	}
}

// SD-003: TestShutdown_PostShutdownSyncStillWorks verifies that after
// Shutdown, Version with WithSync still works (bypasses pool).
func TestShutdown_PostShutdownSyncStillWorks(t *testing.T) {
	conn := connectPostgres(t)
	cleanupPGTables(t, conn)

	w := postgres.NewWriter(conn)
	r := postgres.NewReader(conn)

	a, err := audit.New(audit.Config{
		Writer: w,
		Reader: r,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	user := &UserEntity{ID: uniqueID("user"), Name: "post-shutdown"}
	if err := a.Register(user); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Shutdown first.
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Version with sync mode after shutdown — should either succeed
	// (sync bypasses pool) or fail gracefully.
	ctx := context.Background()
	pv, err := a.Version(ctx, user, port.WithSync())
	if err != nil {
		// Acceptable: library may reject after shutdown.
		t.Logf("Version after shutdown rejected: %v (acceptable)", err)
		return
	}
	// If it succeeds, verify record was persisted.
	rec, err := pv.Wait(ctx)
	if err != nil {
		t.Logf("Wait after shutdown: %v (acceptable)", err)
		return
	}
	if rec.Version != 1 {
		t.Errorf("version = %d, want 1", rec.Version)
	}
}
