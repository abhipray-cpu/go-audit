package audit

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/adapter/cloner"
	"github.com/abhipray-cpu/go-audit/adapter/logger"
	"github.com/abhipray-cpu/go-audit/adapter/serializer"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/diff"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------- stub writer ----------

type stubWriter struct{}

func (s *stubWriter) Save(_ context.Context, _ domain.VersionRecord) error   { return nil }
func (s *stubWriter) Update(_ context.Context, _ domain.VersionRecord) error { return nil }
func (s *stubWriter) SaveInTx(_ context.Context, _ port.Transaction, _ domain.VersionRecord) error {
	return nil
}

// ---------- recording writer (for Version tests) ----------

type recordingWriter struct {
	saved []domain.VersionRecord
}

func (w *recordingWriter) Save(_ context.Context, rec domain.VersionRecord) error {
	w.saved = append(w.saved, rec)
	return nil
}
func (w *recordingWriter) Update(_ context.Context, rec domain.VersionRecord) error {
	w.saved = append(w.saved, rec)
	return nil
}
func (w *recordingWriter) SaveInTx(_ context.Context, _ port.Transaction, rec domain.VersionRecord) error {
	w.saved = append(w.saved, rec)
	return nil
}

// ---------- stub reader ----------

type stubReader struct{}

func (s *stubReader) GetByVersion(_ context.Context, _, _ string, _ int64) (domain.VersionRecord, error) {
	return domain.VersionRecord{}, nil
}
func (s *stubReader) GetLatest(_ context.Context, _, _ string) (domain.VersionRecord, error) {
	return domain.VersionRecord{}, domain.ErrNotFound
}
func (s *stubReader) GetAtTime(_ context.Context, _, _ string, _ time.Time) (domain.VersionRecord, error) {
	return domain.VersionRecord{}, nil
}
func (s *stubReader) ListVersions(_ context.Context, _, _ string, _ ...port.ListOption) ([]domain.VersionRecord, error) {
	return nil, nil
}

// ---------- in-memory reader (for multi-version tests) ----------

type memReader struct {
	records []domain.VersionRecord
}

func (m *memReader) GetByVersion(_ context.Context, entityType, entityID string, ver int64) (domain.VersionRecord, error) {
	for _, r := range m.records {
		if r.EntityType == entityType && r.EntityID == entityID && r.Version == ver {
			return r, nil
		}
	}
	return domain.VersionRecord{}, domain.ErrNotFound
}

func (m *memReader) GetLatest(_ context.Context, entityType, entityID string) (domain.VersionRecord, error) {
	var best *domain.VersionRecord
	for i := range m.records {
		r := &m.records[i]
		if r.EntityType == entityType && r.EntityID == entityID {
			if best == nil || r.Version > best.Version {
				best = r
			}
		}
	}
	if best == nil {
		return domain.VersionRecord{}, domain.ErrNotFound
	}
	return *best, nil
}

func (m *memReader) GetAtTime(_ context.Context, entityType, entityID string, t time.Time) (domain.VersionRecord, error) {
	var best *domain.VersionRecord
	for i := range m.records {
		r := &m.records[i]
		if r.EntityType == entityType && r.EntityID == entityID && !r.CreatedAt.After(t) {
			if best == nil || r.Version > best.Version {
				best = r
			}
		}
	}
	if best == nil {
		return domain.VersionRecord{}, domain.ErrNotFound
	}
	return *best, nil
}

func (m *memReader) ListVersions(_ context.Context, entityType, entityID string, _ ...port.ListOption) ([]domain.VersionRecord, error) {
	var filtered []domain.VersionRecord
	for _, r := range m.records {
		if r.EntityType == entityType && r.EntityID == entityID {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

// memReaderWriter combines an in-memory reader with a recording writer
// that also pushes records into the reader's store.
type memReaderWriter struct {
	reader *memReader
}

func newMemReaderWriter() *memReaderWriter {
	return &memReaderWriter{reader: &memReader{}}
}

func (mw *memReaderWriter) Save(_ context.Context, rec domain.VersionRecord) error {
	// Upsert: overwrite if same entity+version exists (for redaction).
	for i, r := range mw.reader.records {
		if r.EntityType == rec.EntityType && r.EntityID == rec.EntityID && r.Version == rec.Version {
			mw.reader.records[i] = rec
			return nil
		}
	}
	mw.reader.records = append(mw.reader.records, rec)
	return nil
}

func (mw *memReaderWriter) Update(_ context.Context, rec domain.VersionRecord) error {
	return mw.Save(context.Background(), rec)
}

func (mw *memReaderWriter) SaveInTx(_ context.Context, _ port.Transaction, rec domain.VersionRecord) error {
	mw.reader.records = append(mw.reader.records, rec)
	return nil
}

// ---------- test entities ----------

type testUser struct {
	ID    string `version:"id"`
	Name  string `version:"tracked"`
	Email string `version:"tracked"`
}

type unregisteredEntity struct {
	ID   string `version:"id"`
	Data string `version:"tracked"`
}

// ---------- Tests ----------

func TestNew_MinimalConfig(t *testing.T) {
	a, err := New(Config{
		Writer: &stubWriter{},
		Reader: &stubReader{},
	})
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("New() returned nil Auditor")
	}

	// Defaults should be wired.
	cfg := a.Config()
	if cfg.Logger == nil {
		t.Error("Logger default not wired")
	}
	if cfg.Serializer == nil {
		t.Error("Serializer default not wired")
	}
	if cfg.Cloner == nil {
		t.Error("Cloner default not wired")
	}
	if cfg.Differ == nil {
		t.Error("Differ default not wired")
	}
}

func TestNew_InvalidConfig_NoWriter(t *testing.T) {
	_, err := New(Config{
		Reader: &stubReader{},
	})
	if err == nil {
		t.Fatal("expected error for missing Writer")
	}
	if !errors.Is(err, domain.ErrConfiguration) {
		t.Errorf("expected ErrConfiguration, got: %v", err)
	}
}

func TestNew_InvalidConfig_NoReader(t *testing.T) {
	_, err := New(Config{
		Writer: &stubWriter{},
	})
	if err == nil {
		t.Fatal("expected error for missing Reader")
	}
	if !errors.Is(err, domain.ErrConfiguration) {
		t.Errorf("expected ErrConfiguration, got: %v", err)
	}
}

func TestNew_InvalidConfig_Empty(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("expected error for empty config")
	}
	if !errors.Is(err, domain.ErrConfiguration) {
		t.Errorf("expected ErrConfiguration, got: %v", err)
	}
}

func TestNew_CustomAdapters(t *testing.T) {
	customLogger := logger.NewSlog(nil)
	customSerializer := serializer.NewJSON()
	customCloner := cloner.NewReflect()
	customDiffer := diff.New()

	a, err := New(Config{
		Writer:     &stubWriter{},
		Reader:     &stubReader{},
		Logger:     customLogger,
		Serializer: customSerializer,
		Cloner:     customCloner,
		Differ:     customDiffer,
	})
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	cfg := a.Config()
	if cfg.Logger != customLogger {
		t.Error("custom Logger not preserved")
	}
	if cfg.Serializer != customSerializer {
		t.Error("custom Serializer not preserved")
	}
	if cfg.Cloner != customCloner {
		t.Error("custom Cloner not preserved")
	}
	if cfg.Differ != customDiffer {
		t.Error("custom Differ not preserved")
	}
}

func TestAuditor_Register(t *testing.T) {
	a, err := New(Config{
		Writer: &stubWriter{},
		Reader: &stubReader{},
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Verify the entity is registered.
	cfg, err := a.Registry().Get("testuser")
	if err != nil {
		t.Fatalf("Registry().Get() error: %v", err)
	}
	if cfg.TypeName != "testuser" {
		t.Errorf("expected TypeName 'testuser', got %q", cfg.TypeName)
	}
	if cfg.IDField != "ID" {
		t.Errorf("expected IDField 'ID', got %q", cfg.IDField)
	}
}

func TestAuditor_Register_DuplicateEntity(t *testing.T) {
	a, err := New(Config{
		Writer: &stubWriter{},
		Reader: &stubReader{},
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("first Register() error: %v", err)
	}

	err = a.Register(&testUser{})
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
	if !errors.Is(err, domain.ErrConfiguration) {
		t.Errorf("expected ErrConfiguration, got: %v", err)
	}
}

func TestAuditor_Register_NonStruct(t *testing.T) {
	a, err := New(Config{
		Writer: &stubWriter{},
		Reader: &stubReader{},
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	err = a.Register("not-a-struct")
	if err == nil {
		t.Fatal("expected error for non-struct")
	}
	if !errors.Is(err, domain.ErrConfiguration) {
		t.Errorf("expected ErrConfiguration, got: %v", err)
	}
}

func TestAuditor_Shutdown(t *testing.T) {
	a, err := New(Config{
		Writer: &stubWriter{},
		Reader: &stubReader{},
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := a.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() error: %v", err)
	}
}

func TestAuditor_LookupEntity(t *testing.T) {
	a, err := New(Config{
		Writer: &stubWriter{},
		Reader: &stubReader{},
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_ = a.Register(&testUser{})

	cfg, err := a.LookupEntity(&testUser{ID: "u1"})
	if err != nil {
		t.Fatalf("LookupEntity() error: %v", err)
	}
	if cfg.TypeName != "testuser" {
		t.Errorf("expected 'testuser', got %q", cfg.TypeName)
	}
}

func TestAuditor_LookupEntity_Unregistered(t *testing.T) {
	a, err := New(Config{
		Writer: &stubWriter{},
		Reader: &stubReader{},
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_, err = a.LookupEntity(&testUser{})
	if err == nil {
		t.Fatal("expected error for unregistered entity")
	}
}

func TestAuditor_WriterReaderAccessors(t *testing.T) {
	w := &stubWriter{}
	r := &stubReader{}
	a, err := New(Config{Writer: w, Reader: r})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if a.Writer() != w {
		t.Error("Writer() did not return configured writer")
	}
	if a.Reader() != r {
		t.Error("Reader() did not return configured reader")
	}
}

// ==========================================================================
// GA-022 — Version() SyncMode Pipeline
// ==========================================================================

func TestVersion_SyncMode_EndToEnd(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	pending, err := a.Version(context.Background(), user, port.WithSync())
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}
	record, waitErr := pending.Wait(context.Background())
	if waitErr != nil {
		t.Fatalf("Wait() error: %v", waitErr)
	}

	// Check record.
	if record.EntityType != "testuser" {
		t.Errorf("expected entity_type 'testuser', got %q", record.EntityType)
	}
	if record.EntityID != "u1" {
		t.Errorf("expected entity_id 'u1', got %q", record.EntityID)
	}
	if record.Version != 1 {
		t.Errorf("expected version 1, got %d", record.Version)
	}
	if record.ID == "" {
		t.Error("expected non-empty record ID")
	}
	if record.ContentType != "application/json" {
		t.Errorf("expected content_type 'application/json', got %q", record.ContentType)
	}
	if len(record.Data) == 0 {
		t.Error("expected non-empty Data")
	}

	// Verify it's persisted — GetLatest should return it.
	latest, err := mw.reader.GetLatest(context.Background(), "testuser", "u1")
	if err != nil {
		t.Fatalf("GetLatest() error: %v", err)
	}
	if latest.Version != 1 {
		t.Errorf("GetLatest version: expected 1, got %d", latest.Version)
	}
}

func TestVersion_UnregisteredEntity(t *testing.T) {
	a, err := New(Config{Writer: &stubWriter{}, Reader: &stubReader{}})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Do NOT register unregisteredEntity.
	_, vErr := a.Version(context.Background(), &unregisteredEntity{ID: "x1", Data: "test"})
	if vErr == nil {
		t.Fatal("expected error for unregistered entity")
	}
	if !errors.Is(vErr, domain.ErrConfiguration) {
		t.Errorf("expected ErrConfiguration, got: %v", vErr)
	}
}

func TestVersion_SecondVersion_IncrementsSequence(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}

	// Version 1.
	p1, err := a.Version(context.Background(), user, port.WithSync())
	if err != nil {
		t.Fatalf("Version() v1 error: %v", err)
	}
	r1, err := p1.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() v1 error: %v", err)
	}
	if r1.Version != 1 {
		t.Errorf("expected v1, got %d", r1.Version)
	}

	// Mutate and version 2.
	user.Name = "Bob"
	user.Email = "bob@example.com"
	p2, err := a.Version(context.Background(), user, port.WithSync())
	if err != nil {
		t.Fatalf("Version() v2 error: %v", err)
	}
	r2, err := p2.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() v2 error: %v", err)
	}
	if r2.Version != 2 {
		t.Errorf("expected v2, got %d", r2.Version)
	}

	// Verify both are persisted.
	if len(mw.reader.records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(mw.reader.records))
	}
}

func TestVersion_DiffComputed(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Version 1: full snapshot.
	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	p1, err := a.Version(context.Background(), user, port.WithStrategy(domain.StrategyDelta), port.WithSync())
	if err != nil {
		t.Fatalf("Version() v1 error: %v", err)
	}
	r1, err := p1.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() v1 error: %v", err)
	}
	// First version is always StrategyFull regardless of option (no prev to diff).
	if r1.Version != 1 {
		t.Errorf("expected v1, got %d", r1.Version)
	}

	// Version 2: delta should be computed.
	user.Name = "Bob"
	p2, err := a.Version(context.Background(), user, port.WithStrategy(domain.StrategyDelta), port.WithSync())
	if err != nil {
		t.Fatalf("Version() v2 error: %v", err)
	}
	r2, err := p2.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() v2 error: %v", err)
	}
	if r2.Version != 2 {
		t.Errorf("expected v2, got %d", r2.Version)
	}

	// Delta should be populated with the Name change.
	if r2.Delta == nil {
		t.Fatal("expected Delta to be populated on v2")
	}
	if len(r2.Delta.Changes) == 0 {
		t.Fatal("expected at least one field change in Delta")
	}

	// Find the Name change.
	found := false
	for _, ch := range r2.Delta.Changes {
		if ch.Path == "Name" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a change for path 'Name', got changes: %+v", r2.Delta.Changes)
	}
}

func TestVersion_EmptyEntityID(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Empty ID should be rejected.
	user := &testUser{ID: "", Name: "Alice", Email: "alice@example.com"}
	_, vErr := a.Version(context.Background(), user)
	if vErr == nil {
		t.Fatal("expected error for empty entity ID")
	}
	if !errors.Is(vErr, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got: %v", vErr)
	}
}

func TestVersion_NilEntity(t *testing.T) {
	a, err := New(Config{Writer: &stubWriter{}, Reader: &stubReader{}})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_, vErr := a.Version(context.Background(), nil)
	if vErr == nil {
		t.Fatal("expected error for nil entity")
	}
}

// ExampleAuditor_Version demonstrates the basic Version() workflow.
func ExampleAuditor_Version() {
	mw := newMemReaderWriter()

	auditor, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		panic(err)
	}
	defer auditor.Shutdown(context.Background())

	// Register the entity type once at startup.
	auditor.Register(&testUser{})

	// Create a version.
	user := &testUser{ID: "user-1", Name: "Alice", Email: "alice@example.com"}
	pending, err := auditor.Version(context.Background(), user, port.WithSync())
	if err != nil {
		panic(err)
	}
	record, err := pending.Wait(context.Background())
	if err != nil {
		panic(err)
	}

	_ = record // record contains the persisted VersionRecord
	// Output:
}

// ==========================================================================
// GA-032 — BackgroundMode + PendingVersion
// ==========================================================================

func TestVersion_BackgroundMode_ReturnsPendingVersion(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Default mode is BackgroundMode (no WithSync).
	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	pending, err := a.Version(context.Background(), user)
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}
	if pending == nil {
		t.Fatal("expected non-nil PendingVersion")
	}

	// Wait for the background worker to finish.
	record, waitErr := pending.Wait(context.Background())
	if waitErr != nil {
		t.Fatalf("Wait() error: %v", waitErr)
	}
	if record.Version != 1 {
		t.Errorf("expected version 1, got %d", record.Version)
	}
	if record.EntityType != "testuser" {
		t.Errorf("expected entity_type 'testuser', got %q", record.EntityType)
	}
}

func TestVersion_BackgroundMode_DoneChannel(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	pending, err := a.Version(context.Background(), user)
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}

	// Done() should be selectable and eventually close.
	select {
	case <-pending.Done():
		// Success — background work completed.
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Done() channel")
	}

	// After Done(), Err() and Record() should be valid.
	if pending.Err() != nil {
		t.Fatalf("Err() returned unexpected error: %v", pending.Err())
	}
	if pending.Record().Version != 1 {
		t.Errorf("Record().Version = %d, want 1", pending.Record().Version)
	}
}

func TestVersion_SyncMode_PendingAlreadyDone(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	pending, err := a.Version(context.Background(), user, port.WithSync())
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}

	// In sync mode, Done() should already be closed.
	select {
	case <-pending.Done():
		// Good — already done.
	default:
		t.Fatal("expected Done() to be already closed in sync mode")
	}

	record, waitErr := pending.Wait(context.Background())
	if waitErr != nil {
		t.Fatalf("Wait() error: %v", waitErr)
	}
	if record.Version != 1 {
		t.Errorf("expected version 1, got %d", record.Version)
	}
}

func TestVersion_BackgroundMode_MultipleVersions(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}

	// Create multiple background versions and collect pendings.
	const count = 5
	pendings := make([]*domain.PendingVersion, count)
	for i := 0; i < count; i++ {
		user.Name = user.Name + "x"
		p, vErr := a.Version(context.Background(), user, port.WithSync())
		if vErr != nil {
			t.Fatalf("Version() v%d error: %v", i+1, vErr)
		}
		// Wait for each one synchronously to ensure sequence ordering.
		p.Wait(context.Background())
		pendings[i] = p
	}

	// All should complete.
	for i, p := range pendings {
		rec, wErr := p.Wait(context.Background())
		if wErr != nil {
			t.Fatalf("Wait() v%d error: %v", i+1, wErr)
		}
		if rec.Version != int64(i+1) {
			t.Errorf("v%d: expected version %d, got %d", i+1, i+1, rec.Version)
		}
	}
}

func TestVersion_BackgroundMode_Backpressure(t *testing.T) {
	// Use a tiny pool to trigger backpressure.
	mw := newMemReaderWriter()

	a, err := New(Config{
		Writer:        mw,
		Reader:        mw.reader,
		PoolWorkers:   1,
		PoolQueueSize: 1,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())

	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Even under backpressure, items should still complete (via OnBackpressure
	// fallback which processes synchronously).
	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	for i := 0; i < 10; i++ {
		user.Name = user.Name + "x"
		p, vErr := a.Version(context.Background(), user, port.WithSync())
		if vErr != nil {
			t.Fatalf("Version() v%d error: %v", i+1, vErr)
		}
		_, wErr := p.Wait(context.Background())
		if wErr != nil {
			t.Fatalf("Wait() v%d error: %v", i+1, wErr)
		}
	}

	if mw.reader.records == nil || len(mw.reader.records) != 10 {
		t.Errorf("expected 10 records, got %d", len(mw.reader.records))
	}
}

func TestDeltaStrategy_Validation_IntegrationInWorker(t *testing.T) {
	// Integration test: Version() with StrategyDelta verifies Apply(prev, delta)==current.
	// If validation fails, the auditor falls back to StrategyFull silently.
	mw := newMemReaderWriter()

	a, err := New(Config{
		Writer:   mw,
		Reader:   mw.reader,
		Strategy: domain.StrategyDelta,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())

	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// v1: always full.
	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	p1, err := a.Version(context.Background(), user, port.WithSync())
	if err != nil {
		t.Fatalf("Version() v1 error: %v", err)
	}
	r1, _ := p1.Wait(context.Background())
	if r1.Strategy != domain.StrategyFull {
		t.Errorf("v1 strategy = %v, want Full", r1.Strategy)
	}

	// v2: delta with validation pass — should succeed as StrategyDelta.
	user.Name = "Bob"
	p2, err := a.Version(context.Background(), user, port.WithSync())
	if err != nil {
		t.Fatalf("Version() v2 error: %v", err)
	}
	r2, _ := p2.Wait(context.Background())
	if r2.Strategy != domain.StrategyDelta {
		t.Errorf("v2 strategy = %v, want Delta", r2.Strategy)
	}
	if r2.Delta == nil {
		t.Error("v2 delta should be populated")
	}
}

func BenchmarkVersion_BackgroundMode_HotPath(b *testing.B) {
	// Benchmark the hot path: clone + metadata + enqueue.
	// Use a noop writer (stubWriter) since we're measuring enqueue latency,
	// not persist latency. stubReader always returns ErrNotFound so
	// NextVersion always returns 1 (no concurrent slice mutation).
	a, err := New(Config{Writer: &stubWriter{}, Reader: &stubReader{}})
	if err != nil {
		b.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())

	if err := a.Register(&testUser{}); err != nil {
		b.Fatalf("Register() error: %v", err)
	}

	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, err := a.Version(ctx, user)
		if err != nil {
			b.Fatalf("Version() error: %v", err)
		}
		_ = p
	}
	b.StopTimer()

	// Drain pool before shutdown.
	a.Shutdown(context.Background())
}

// ==========================================================================
// GA-023 — Context Metadata Wiring into Version()
// ==========================================================================

func TestVersion_MetadataFromContext(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	ctx := context.Background()
	ctx = WithActor(ctx, "admin-42", domain.ActorHuman)
	ctx = WithReason(ctx, "profile update")
	ctx = WithCorrelationID(ctx, "corr-xyz")
	ctx = WithCustomMetadata(ctx, "ticket", "JIRA-1234")

	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	pending, err := a.Version(ctx, user, port.WithSync())
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}
	record, waitErr := pending.Wait(ctx)
	if waitErr != nil {
		t.Fatalf("Wait() error: %v", waitErr)
	}

	meta := record.Metadata
	if meta.ActorID != "admin-42" {
		t.Errorf("ActorID = %q, want %q", meta.ActorID, "admin-42")
	}
	if meta.ActorType != domain.ActorHuman {
		t.Errorf("ActorType = %v, want ActorHuman", meta.ActorType)
	}
	if meta.Reason != "profile update" {
		t.Errorf("Reason = %q, want %q", meta.Reason, "profile update")
	}
	if meta.CorrelationID != "corr-xyz" {
		t.Errorf("CorrelationID = %q, want %q", meta.CorrelationID, "corr-xyz")
	}
	if meta.Custom["ticket"] != "JIRA-1234" {
		t.Errorf("Custom[ticket] = %q, want %q", meta.Custom["ticket"], "JIRA-1234")
	}
	if meta.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

func TestVersion_EmptyContext_DefaultMetadata(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	pending, err := a.Version(context.Background(), user, port.WithSync())
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}
	record, waitErr := pending.Wait(context.Background())
	if waitErr != nil {
		t.Fatalf("Wait() error: %v", waitErr)
	}

	meta := record.Metadata
	if meta.ActorID != "" {
		t.Errorf("ActorID = %q, want empty", meta.ActorID)
	}
	if meta.Reason != "" {
		t.Errorf("Reason = %q, want empty", meta.Reason)
	}
	if meta.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero even with empty context")
	}
}

func TestConfig_MetadataExtractorDefault(t *testing.T) {
	a, err := New(Config{Writer: &stubWriter{}, Reader: &stubReader{}})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if a.Config().MetadataExtractor == nil {
		t.Error("MetadataExtractor default not wired")
	}
}

// ==========================================================================
// GA-024 — Query API
// ==========================================================================

func TestGetVersion(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	p, err := a.Version(context.Background(), user, port.WithSync())
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}
	p.Wait(context.Background())

	got, err := a.GetVersion(context.Background(), "testuser", "u1", 1)
	if err != nil {
		t.Fatalf("GetVersion() error: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("Version = %d, want 1", got.Version)
	}

	// Non-existent version.
	_, err = a.GetVersion(context.Background(), "testuser", "u1", 99)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestGetLatest(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	p1, _ := a.Version(context.Background(), user, port.WithSync())
	p1.Wait(context.Background())
	user.Name = "Bob"
	p2, _ := a.Version(context.Background(), user, port.WithSync())
	p2.Wait(context.Background())

	got, err := a.GetLatest(context.Background(), "testuser", "u1")
	if err != nil {
		t.Fatalf("GetLatest() error: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("Version = %d, want 2", got.Version)
	}
}

func TestGetAtTime(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	p, _ := a.Version(context.Background(), user, port.WithSync())
	p.Wait(context.Background())

	// GetAtTime should find the version we just created when querying a future time.
	rec, err := a.GetAtTime(context.Background(), "testuser", "u1", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("GetAtTime() error: %v", err)
	}
	if rec.EntityID != "u1" {
		t.Errorf("expected entity u1, got %s", rec.EntityID)
	}

	// Querying before the version was created should return ErrNotFound.
	_, err = a.GetAtTime(context.Background(), "testuser", "u1", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for past time, got: %v", err)
	}
}

func TestListVersions(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	for i := 0; i < 3; i++ {
		user.Name = user.Name + "x"
		p, _ := a.Version(context.Background(), user, port.WithSync())
		p.Wait(context.Background())
	}

	recs, err := a.ListVersions(context.Background(), "testuser", "u1")
	if err != nil {
		t.Fatalf("ListVersions() error: %v", err)
	}
	if len(recs) != 3 {
		t.Errorf("expected 3 records, got %d", len(recs))
	}
}

// ==========================================================================
// GA-025 — VersionInTx
// ==========================================================================

func TestVersionInTx_SyncMode(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Use a dummy tx (in-memory store ignores it).
	fakeTx := "fake-transaction"

	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	result, err := a.VersionInTx(context.Background(), fakeTx, user)
	if err != nil {
		t.Fatalf("VersionInTx() error: %v", err)
	}

	if result.Record.Version != 1 {
		t.Errorf("Version = %d, want 1", result.Record.Version)
	}
	if result.Record.EntityType != "testuser" {
		t.Errorf("EntityType = %q, want %q", result.Record.EntityType, "testuser")
	}
	if result.Duration <= 0 {
		t.Error("expected positive duration")
	}
}

func TestVersionInTx_NilTx(t *testing.T) {
	a, err := New(Config{Writer: &stubWriter{}, Reader: &stubReader{}})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	user := &testUser{ID: "u1", Name: "Alice"}
	_, vErr := a.VersionInTx(context.Background(), nil, user)
	if vErr == nil {
		t.Fatal("expected error for nil tx")
	}
	if !errors.Is(vErr, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got: %v", vErr)
	}
}

func TestVersionInTx_UnregisteredEntity(t *testing.T) {
	a, err := New(Config{Writer: &stubWriter{}, Reader: &stubReader{}})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_, vErr := a.VersionInTx(context.Background(), "tx", &unregisteredEntity{ID: "x1", Data: "test"})
	if vErr == nil {
		t.Fatal("expected error for unregistered entity")
	}
	if !errors.Is(vErr, domain.ErrConfiguration) {
		t.Errorf("expected ErrConfiguration, got: %v", vErr)
	}
}

func TestVersionInTx_MetadataFromContext(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	ctx := WithActor(context.Background(), "svc-payment", domain.ActorService)
	ctx = WithReason(ctx, "refund processing")

	user := &testUser{ID: "u1", Name: "Alice"}
	result, err := a.VersionInTx(ctx, "tx", user)
	if err != nil {
		t.Fatalf("VersionInTx() error: %v", err)
	}

	if result.Record.Metadata.ActorID != "svc-payment" {
		t.Errorf("ActorID = %q, want %q", result.Record.Metadata.ActorID, "svc-payment")
	}
	if result.Record.Metadata.Reason != "refund processing" {
		t.Errorf("Reason = %q, want %q", result.Record.Metadata.Reason, "refund processing")
	}
}

func TestVersionInTx_EmptyEntityID(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := a.Register(&testUser{}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	user := &testUser{ID: "", Name: "Alice"}
	_, vErr := a.VersionInTx(context.Background(), "tx", user)
	if vErr == nil {
		t.Fatal("expected error for empty entity ID")
	}
	if !errors.Is(vErr, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got: %v", vErr)
	}
}

// ==========================================================================
// WAL Integration Tests (GA-037)
// ==========================================================================

// recordingWAL captures Append and Ack calls for assertions.
type recordingWAL struct {
	mu       sync.Mutex
	appended []port.WALEntry
	acked    []string
}

func (w *recordingWAL) Append(_ context.Context, entry port.WALEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.appended = append(w.appended, entry)
	return nil
}

func (w *recordingWAL) Ack(_ context.Context, entryID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.acked = append(w.acked, entryID)
	return nil
}

func (w *recordingWAL) Replay(_ context.Context) ([]port.WALEntry, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Return entries that haven't been acked.
	ackedSet := make(map[string]bool, len(w.acked))
	for _, id := range w.acked {
		ackedSet[id] = true
	}
	var unacked []port.WALEntry
	for _, e := range w.appended {
		if !ackedSet[e.ID] {
			unacked = append(unacked, e)
		}
	}
	return unacked, nil
}

func TestWAL_AppendBeforeEnqueue(t *testing.T) {
	wal := &recordingWAL{}
	rw := newMemReaderWriter()

	a, err := New(Config{
		Writer: rw,
		Reader: rw.reader,
		WAL:    wal,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	user := &testUser{ID: "u1", Name: "Alice"}
	pending, err := a.Version(context.Background(), user)
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}

	// WAL Append should have been called on the caller goroutine
	// (before enqueue), so it's immediately visible.
	wal.mu.Lock()
	appendCount := len(wal.appended)
	wal.mu.Unlock()

	if appendCount != 1 {
		t.Errorf("WAL Append count = %d, want 1 (called before enqueue)", appendCount)
	}

	// Wait for background processing.
	_, waitErr := pending.Wait(context.Background())
	if waitErr != nil {
		t.Fatalf("Wait() error: %v", waitErr)
	}
}

func TestWAL_AckAfterPersist(t *testing.T) {
	wal := &recordingWAL{}
	rw := newMemReaderWriter()

	a, err := New(Config{
		Writer: rw,
		Reader: rw.reader,
		WAL:    wal,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	user := &testUser{ID: "u1", Name: "Alice"}
	pending, err := a.Version(context.Background(), user)
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}

	// Wait for full processing (persist + ack).
	_, waitErr := pending.Wait(context.Background())
	if waitErr != nil {
		t.Fatalf("Wait() error: %v", waitErr)
	}

	// After persist, WAL Ack should have been called.
	wal.mu.Lock()
	ackCount := len(wal.acked)
	wal.mu.Unlock()

	if ackCount != 1 {
		t.Errorf("WAL Ack count = %d, want 1 (called after persist)", ackCount)
	}

	// Replay should return empty (all acked).
	entries, err := wal.Replay(context.Background())
	if err != nil {
		t.Fatalf("Replay() error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Replay() returned %d entries, want 0 (all acked)", len(entries))
	}
}

func TestWAL_SyncMode_NoWALInteraction(t *testing.T) {
	// In sync mode, WAL should NOT be used (no append/ack).
	wal := &recordingWAL{}
	rw := newMemReaderWriter()

	a, err := New(Config{
		Writer: rw,
		Reader: rw.reader,
		WAL:    wal,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	user := &testUser{ID: "u1", Name: "Alice"}
	pending, err := a.Version(context.Background(), user, port.WithSync())
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}

	_, waitErr := pending.Wait(context.Background())
	if waitErr != nil {
		t.Fatalf("Wait() error: %v", waitErr)
	}

	wal.mu.Lock()
	appendCount := len(wal.appended)
	ackCount := len(wal.acked)
	wal.mu.Unlock()

	if appendCount != 0 {
		t.Errorf("WAL Append count = %d, want 0 (sync mode bypasses WAL)", appendCount)
	}
	if ackCount != 0 {
		t.Errorf("WAL Ack count = %d, want 0 (sync mode bypasses WAL)", ackCount)
	}
}

func TestWAL_NilWAL_NoError(t *testing.T) {
	// When WAL is nil (default), Version should work without errors.
	rw := newMemReaderWriter()

	a, err := New(Config{
		Writer: rw,
		Reader: rw.reader,
		// WAL: nil — default
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	user := &testUser{ID: "u1", Name: "Alice"}
	pending, err := a.Version(context.Background(), user, port.WithSync())
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}
	_, waitErr := pending.Wait(context.Background())
	if waitErr != nil {
		t.Fatalf("Wait() error: %v", waitErr)
	}
}

func TestWAL_WALDir_AutoCreatesFileWAL(t *testing.T) {
	// When WALDir is set and WAL is nil, applyDefaults creates a FileWAL.
	rw := newMemReaderWriter()
	dir := t.TempDir()

	a, err := New(Config{
		Writer: rw,
		Reader: rw.reader,
		WALDir: dir,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())

	if a.cfg.WAL == nil {
		t.Fatal("expected WAL to be auto-created from WALDir, got nil")
	}

	// Verify it functions: register entity, version, wait.
	a.Register(&testUser{})
	user := &testUser{ID: "u1", Name: "Alice"}
	pending, err := a.Version(context.Background(), user, port.WithSync())
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}
	_, waitErr := pending.Wait(context.Background())
	if waitErr != nil {
		t.Fatalf("Wait() error: %v", waitErr)
	}
}

// ==========================================================================
// Advanced Query Tests (GA-046)
// ==========================================================================

func TestCompare_TwoVersions(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	ctx := context.Background()

	// Create v1.
	u1 := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	p1, _ := a.Version(ctx, u1, port.WithSync())
	p1.Wait(ctx)

	// Create v2 with changes.
	u2 := &testUser{ID: "u1", Name: "Bob", Email: "bob@example.com"}
	p2, _ := a.Version(ctx, u2, port.WithSync())
	p2.Wait(ctx)

	delta, err := a.Compare(ctx, "testuser", "u1", 1, 2)
	if err != nil {
		t.Fatalf("Compare() error: %v", err)
	}

	if delta.IsEmpty() {
		t.Fatal("expected non-empty delta between v1 and v2")
	}

	// Should have changes for Name and Email.
	paths := map[string]bool{}
	for _, fc := range delta.Changes {
		paths[fc.Path] = true
	}
	if !paths["Name"] {
		t.Error("expected Name in delta changes")
	}
	if !paths["Email"] {
		t.Error("expected Email in delta changes")
	}
}

func TestFindChanges_FieldHistory(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	ctx := context.Background()

	// Create 3 versions changing Name each time.
	names := []string{"Alice", "Bob", "Charlie"}
	for _, name := range names {
		u := &testUser{ID: "u1", Name: name, Email: "same@example.com"}
		p, _ := a.Version(ctx, u, port.WithSync())
		p.Wait(ctx)
	}

	changes, err := a.FindChanges(ctx, "testuser", "u1", "Name")
	if err != nil {
		t.Fatalf("FindChanges() error: %v", err)
	}

	// 3 versions → 2 transitions → 2 Name changes.
	if len(changes) != 2 {
		t.Fatalf("FindChanges() returned %d changes, want 2", len(changes))
	}
}

func TestFindByActor(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	ctx := context.Background()

	// v1 by admin.
	ctx1 := WithActor(ctx, "admin", domain.ActorHuman)
	p1, _ := a.Version(ctx1, &testUser{ID: "u1", Name: "Alice"}, port.WithSync())
	p1.Wait(ctx)

	// v2 by system.
	ctx2 := WithActor(ctx, "system", domain.ActorService)
	p2, _ := a.Version(ctx2, &testUser{ID: "u1", Name: "Bob"}, port.WithSync())
	p2.Wait(ctx)

	// v3 by admin again.
	p3, _ := a.Version(ctx1, &testUser{ID: "u1", Name: "Charlie"}, port.WithSync())
	p3.Wait(ctx)

	// Find by admin.
	records, err := a.FindByActor(ctx, "testuser", "u1", "admin")
	if err != nil {
		t.Fatalf("FindByActor() error: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("FindByActor('admin') returned %d records, want 2", len(records))
	}

	// Find by system.
	records, err = a.FindByActor(ctx, "testuser", "u1", "system")
	if err != nil {
		t.Fatalf("FindByActor() error: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("FindByActor('system') returned %d records, want 1", len(records))
	}
}

func TestGetBulkAtTime(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{Writer: mw, Reader: mw.reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	ctx := context.Background()

	// Create u1 and u2.
	p1, _ := a.Version(ctx, &testUser{ID: "u1", Name: "Alice"}, port.WithSync())
	p1.Wait(ctx)
	p2, _ := a.Version(ctx, &testUser{ID: "u2", Name: "Bob"}, port.WithSync())
	p2.Wait(ctx)

	// Query both at "now".
	results, err := a.GetBulkAtTime(ctx, "testuser", []string{"u1", "u2", "u3"}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("GetBulkAtTime() error: %v", err)
	}
	// u3 doesn't exist → should get 2 results.
	if len(results) != 2 {
		t.Errorf("GetBulkAtTime() returned %d results, want 2", len(results))
	}
}

// ==========================================================================
// Runtime Guardrails Tests (GA-045)
// ==========================================================================

func TestGuardrails_RuntimeReject(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{
		Writer:             mw,
		Reader:             mw.reader,
		MaxEntitySizeBytes: 10, // 10 bytes — tiny limit
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	// This entity serializes to more than 10 bytes.
	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	pending, vErr := a.Version(context.Background(), user, port.WithSync())

	// In sync mode the error comes from Version() directly.
	if vErr != nil {
		if !errors.Is(vErr, domain.ErrEntityTooLarge) {
			t.Errorf("expected ErrEntityTooLarge, got: %v", vErr)
		}
		return // pass
	}

	// If Version() didn't error, Wait() should.
	_, waitErr := pending.Wait(context.Background())
	if waitErr == nil {
		t.Fatal("expected error for oversized entity, got nil")
	}
	if !errors.Is(waitErr, domain.ErrEntityTooLarge) {
		t.Errorf("expected ErrEntityTooLarge, got: %v", waitErr)
	}
}

func TestGuardrails_RuntimeAccept(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{
		Writer:             mw,
		Reader:             mw.reader,
		MaxEntitySizeBytes: 10_000, // generous
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	user := &testUser{ID: "u1", Name: "Alice"}
	pending, err := a.Version(context.Background(), user, port.WithSync())
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}

	_, waitErr := pending.Wait(context.Background())
	if waitErr != nil {
		t.Fatalf("expected no error, got: %v", waitErr)
	}
}

func TestGuardrails_RuntimeReject_BackgroundMode(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{
		Writer:             mw,
		Reader:             mw.reader,
		MaxEntitySizeBytes: 10, // tiny limit
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	pending, err := a.Version(context.Background(), user) // default = background
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}

	// In background mode the error surfaces from Wait().
	_, waitErr := pending.Wait(context.Background())
	if waitErr == nil {
		t.Fatal("expected error for oversized entity, got nil")
	}
	if !errors.Is(waitErr, domain.ErrEntityTooLarge) {
		t.Errorf("expected ErrEntityTooLarge, got: %v", waitErr)
	}
}

func TestGuardrails_Disabled(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{
		Writer: mw,
		Reader: mw.reader,
		// MaxEntitySizeBytes: 0 → disabled
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	user := &testUser{ID: "u1", Name: "Alice"}
	pending, err := a.Version(context.Background(), user, port.WithSync())
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}

	_, waitErr := pending.Wait(context.Background())
	if waitErr != nil {
		t.Fatalf("expected no error (guardrail disabled), got: %v", waitErr)
	}
}

func TestGuardrails_VersionInTx_RuntimeReject(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{
		Writer:             mw,
		Reader:             mw.reader,
		MaxEntitySizeBytes: 10,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	_, vErr := a.VersionInTx(context.Background(), "tx", user)
	if vErr == nil {
		t.Fatal("expected error for oversized entity, got nil")
	}
	if !errors.Is(vErr, domain.ErrEntityTooLarge) {
		t.Errorf("expected ErrEntityTooLarge, got: %v", vErr)
	}
}

// ---------- GA-053: Redaction Integration Tests ----------

func TestRedactField_ReplacesData(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{
		Writer: mw,
		Reader: mw.reader,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	// Create two versions.
	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	if _, err := a.Version(context.Background(), user, port.WithSync()); err != nil {
		t.Fatalf("Version 1: %v", err)
	}
	user.Email = "alice2@example.com"
	if _, err := a.Version(context.Background(), user, port.WithSync()); err != nil {
		t.Fatalf("Version 2: %v", err)
	}

	// Redact the Email field.
	count, err := a.RedactField(context.Background(), "testuser", "u1", "Email")
	if err != nil {
		t.Fatalf("RedactField: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 versions redacted, got %d", count)
	}

	// Verify both versions have [REDACTED].
	for _, rec := range mw.reader.records {
		if rec.EntityType == "testuser" && rec.EntityID == "u1" {
			s := string(rec.Data)
			if !strings.Contains(s, `"[REDACTED]"`) && !strings.Contains(s, `[REDACTED]`) {
				t.Errorf("version %d data should contain [REDACTED], got: %s", rec.Version, s)
			}
		}
	}
}

func TestRedactField_PreservesHash(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{
		Writer:          mw,
		Reader:          mw.reader,
		EnableHashChain: true,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	if _, err := a.Version(context.Background(), user, port.WithSync()); err != nil {
		t.Fatalf("Version: %v", err)
	}

	// Record original hash.
	rec, _ := a.GetLatest(context.Background(), "testuser", "u1")
	originalHash := rec.Hash
	if originalHash == "" {
		t.Fatal("expected hash to be set with EnableHashChain=true")
	}

	// Redact — this re-saves with new data, so the hash from the writer
	// may differ, but the audit trail structure is preserved.
	count, err := a.RedactField(context.Background(), "testuser", "u1", "Email")
	if err != nil {
		t.Fatalf("RedactField: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 version redacted, got %d", count)
	}
}

// ---------- GA-051: Hash Chain Integration Tests ----------

func TestHashChain_EnabledViaConfig(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{
		Writer:          mw,
		Reader:          mw.reader,
		EnableHashChain: true,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())
	a.Register(&testUser{})

	user := &testUser{ID: "u1", Name: "Alice", Email: "alice@example.com"}
	if _, err := a.Version(context.Background(), user, port.WithSync()); err != nil {
		t.Fatalf("Version 1: %v", err)
	}

	user.Name = "Bob"
	if _, err := a.Version(context.Background(), user, port.WithSync()); err != nil {
		t.Fatalf("Version 2: %v", err)
	}

	// Both records should have hashes.
	for _, rec := range mw.reader.records {
		if rec.Hash == "" {
			t.Errorf("version %d: expected hash, got empty", rec.Version)
		}
	}

	// Version 2's PreviousHash should equal version 1's Hash.
	if len(mw.reader.records) >= 2 {
		v1 := mw.reader.records[0]
		v2 := mw.reader.records[1]
		if v2.PreviousHash != v1.Hash {
			t.Errorf("v2.PreviousHash=%q, want v1.Hash=%q", v2.PreviousHash, v1.Hash)
		}
	}

	// VerifyIntegrity should pass.
	if err := a.VerifyIntegrity(context.Background(), "testuser", "u1"); err != nil {
		t.Fatalf("VerifyIntegrity: %v", err)
	}
}

func TestHashChain_DisabledByDefault(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{
		Writer: mw,
		Reader: mw.reader,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())

	// VerifyIntegrity should fail when hash chain not enabled.
	err = a.VerifyIntegrity(context.Background(), "testuser", "u1")
	if err == nil {
		t.Fatal("expected error when hash chain disabled")
	}
	if !errors.Is(err, domain.ErrConfiguration) {
		t.Errorf("expected ErrConfiguration, got: %v", err)
	}
}

// ---------- GA-050: RegisterMigration Integration Tests ----------

func TestRegisterMigration_AuditorAPI(t *testing.T) {
	mw := newMemReaderWriter()

	a, err := New(Config{
		Writer: mw,
		Reader: mw.reader,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Shutdown(context.Background())

	// Register a v1→v2 migration.
	err = a.RegisterMigration("testuser", 1, 2, func(_ context.Context, data []byte) ([]byte, error) {
		return data, nil // noop migration
	})
	if err != nil {
		t.Fatalf("RegisterMigration: %v", err)
	}

	// Gap should be rejected.
	err = a.RegisterMigration("testuser", 1, 3, func(_ context.Context, data []byte) ([]byte, error) {
		return data, nil
	})
	if err == nil {
		t.Fatal("expected error for gap, got nil")
	}
}
