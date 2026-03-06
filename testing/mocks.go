package audittest

import (
	"context"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------------------------------------------------------------------------
// Compile-time interface satisfaction checks.
// These ensure every mock struct implements its corresponding port.
// ---------------------------------------------------------------------------

// Outbound ports (15)
var _ port.VersionWriterPort = (*MockVersionWriter)(nil)
var _ port.VersionReaderPort = (*MockVersionReader)(nil)
var _ port.CachePort = (*MockCache)(nil)
var _ port.OLAPPort = (*MockOLAP)(nil)
var _ port.ColdStorePort = (*MockColdStore)(nil)
var _ port.WALPort = (*MockWAL)(nil)
var _ port.SerializerPort = (*MockSerializer)(nil)
var _ port.DifferPort = (*MockDiffer)(nil)
var _ port.ClonerPort = (*MockCloner)(nil)
var _ port.InstrumenterPort = (*MockInstrumenter)(nil)
var _ port.SubscriberPort = (*MockSubscriber)(nil)
var _ port.MetadataExtractorPort = (*MockMetadataExtractor)(nil)
var _ port.LoggerPort = (*MockLogger)(nil)
var _ port.HashComputerPort = (*MockHashComputer)(nil)
var _ port.HealthCheckPort = (*MockHealthCheck)(nil)

// Outbound schema port (1)
var _ port.SchemaManagerPort = (*MockSchemaManager)(nil)

// Inbound ports (3)
var _ port.VersioningPort = (*MockVersioning)(nil)
var _ port.QueryPort = (*MockQuery)(nil)
var _ port.SchemaPort = (*MockSchema)(nil)

// ---------------------------------------------------------------------------
// Outbound port mocks
// ---------------------------------------------------------------------------

// MockVersionWriter is a minimal mock for VersionWriterPort.
type MockVersionWriter struct{}

func (m *MockVersionWriter) Save(_ context.Context, _ domain.VersionRecord) error   { return nil }
func (m *MockVersionWriter) Update(_ context.Context, _ domain.VersionRecord) error { return nil }
func (m *MockVersionWriter) SaveInTx(_ context.Context, _ port.Transaction, _ domain.VersionRecord) error {
	return nil
}

// MockVersionReader is a minimal mock for VersionReaderPort.
type MockVersionReader struct{}

func (m *MockVersionReader) GetByVersion(_ context.Context, _, _ string, _ int64) (domain.VersionRecord, error) {
	return domain.VersionRecord{}, nil
}
func (m *MockVersionReader) GetLatest(_ context.Context, _, _ string) (domain.VersionRecord, error) {
	return domain.VersionRecord{}, nil
}
func (m *MockVersionReader) GetAtTime(_ context.Context, _, _ string, _ time.Time) (domain.VersionRecord, error) {
	return domain.VersionRecord{}, nil
}
func (m *MockVersionReader) ListVersions(_ context.Context, _, _ string, _ ...port.ListOption) ([]domain.VersionRecord, error) {
	return nil, nil
}

// MockCache is a minimal mock for CachePort.
type MockCache struct{}

func (m *MockCache) Get(_ context.Context, _, _ string, _ int64) (domain.VersionRecord, bool) {
	return domain.VersionRecord{}, false
}
func (m *MockCache) Put(_ context.Context, _ domain.VersionRecord) {}
func (m *MockCache) Invalidate(_ context.Context, _, _ string)     {}

// MockOLAP is a minimal mock for OLAPPort.
type MockOLAP struct{}

func (m *MockOLAP) BatchInsert(_ context.Context, _ []domain.VersionRecord) error { return nil }
func (m *MockOLAP) Query(_ context.Context, _ string, _ ...port.ListOption) ([]domain.VersionRecord, error) {
	return nil, nil
}

// MockColdStore is a minimal mock for ColdStorePort.
type MockColdStore struct{}

func (m *MockColdStore) Put(_ context.Context, _ string, _ []byte) error { return nil }
func (m *MockColdStore) Get(_ context.Context, _ string) ([]byte, error) { return nil, nil }

// MockWAL is a minimal mock for WALPort.
type MockWAL struct{}

func (m *MockWAL) Append(_ context.Context, _ port.WALEntry) error   { return nil }
func (m *MockWAL) Ack(_ context.Context, _ string) error             { return nil }
func (m *MockWAL) Replay(_ context.Context) ([]port.WALEntry, error) { return nil, nil }

// MockSerializer is a minimal mock for SerializerPort.
type MockSerializer struct{}

func (m *MockSerializer) Marshal(_ context.Context, _ any) ([]byte, error) { return nil, nil }
func (m *MockSerializer) Unmarshal(_ context.Context, _ []byte, _ any) error {
	return nil
}
func (m *MockSerializer) ContentType() string { return "application/json" }

// MockDiffer is a minimal mock for DifferPort.
type MockDiffer struct{}

func (m *MockDiffer) Diff(_ context.Context, _, _ any) (domain.Delta, error) {
	return domain.Delta{}, nil
}
func (m *MockDiffer) Apply(_ context.Context, _ any, _ domain.Delta) (any, error) { return nil, nil }

// MockCloner is a minimal mock for ClonerPort.
type MockCloner struct{}

func (m *MockCloner) Clone(_ context.Context, _ any) (any, error) { return nil, nil }

// MockInstrumenter is a minimal mock for InstrumenterPort.
type MockInstrumenter struct{}

func (m *MockInstrumenter) StartSpan(_ context.Context, _ string) (context.Context, port.Span) {
	return context.Background(), &mockSpan{}
}
func (m *MockInstrumenter) EndSpan(_ port.Span) {}
func (m *MockInstrumenter) RecordMetric(_ context.Context, _ string, _ float64, _ map[string]string) {
}

// mockSpan is a noop Span implementation used by MockInstrumenter.
type mockSpan struct{}

func (s *mockSpan) End()                         {}
func (s *mockSpan) SetAttribute(_ string, _ any) {}

// MockSubscriber is a minimal mock for SubscriberPort.
type MockSubscriber struct{}

func (m *MockSubscriber) OnVersion(_ context.Context, _ domain.VersionRecord) error { return nil }

// MockMetadataExtractor is a minimal mock for MetadataExtractorPort.
type MockMetadataExtractor struct{}

func (m *MockMetadataExtractor) Extract(_ context.Context) domain.VersionMetadata {
	return domain.VersionMetadata{}
}

// MockLogger is a minimal mock for LoggerPort.
type MockLogger struct{}

func (m *MockLogger) Info(_ string, _ ...any)  {}
func (m *MockLogger) Warn(_ string, _ ...any)  {}
func (m *MockLogger) Error(_ string, _ ...any) {}
func (m *MockLogger) Debug(_ string, _ ...any) {}

// MockHashComputer is a minimal mock for HashComputerPort.
type MockHashComputer struct{}

func (m *MockHashComputer) Compute(_ context.Context, _ []byte) (string, error) { return "", nil }

// MockHealthCheck is a minimal mock for HealthCheckPort.
type MockHealthCheck struct{}

func (m *MockHealthCheck) Check(_ context.Context) []port.HealthStatus { return nil }

// MockSchemaManager is a minimal mock for SchemaManagerPort.
type MockSchemaManager struct{}

func (m *MockSchemaManager) Migrate(_ context.Context) error               { return nil }
func (m *MockSchemaManager) CurrentVersion(_ context.Context) (int, error) { return 0, nil }

// ---------------------------------------------------------------------------
// Inbound port mocks
// ---------------------------------------------------------------------------

// MockVersioning is a minimal mock for VersioningPort.
type MockVersioning struct{}

func (m *MockVersioning) Version(_ context.Context, _ any, _ ...port.VersionOption) (*domain.PendingVersion, error) {
	return domain.NewPendingVersion(), nil
}
func (m *MockVersioning) VersionInTx(_ context.Context, _ port.Transaction, _ any) (domain.VersionRecord, error) {
	return domain.VersionRecord{}, nil
}
func (m *MockVersioning) Register(_ any) error             { return nil }
func (m *MockVersioning) Shutdown(_ context.Context) error { return nil }

// MockQuery is a minimal mock for QueryPort.
type MockQuery struct{}

func (m *MockQuery) GetVersion(_ context.Context, _, _ string, _ int64) (domain.VersionRecord, error) {
	return domain.VersionRecord{}, nil
}
func (m *MockQuery) GetLatest(_ context.Context, _, _ string) (domain.VersionRecord, error) {
	return domain.VersionRecord{}, nil
}
func (m *MockQuery) GetAtTime(_ context.Context, _, _ string, _ time.Time) (domain.VersionRecord, error) {
	return domain.VersionRecord{}, nil
}
func (m *MockQuery) ListVersions(_ context.Context, _, _ string, _ ...port.ListOption) ([]domain.VersionRecord, error) {
	return nil, nil
}
func (m *MockQuery) Compare(_ context.Context, _, _ string, _, _ int64) (domain.Delta, error) {
	return domain.Delta{}, nil
}
func (m *MockQuery) FindChanges(_ context.Context, _, _, _ string, _ ...port.ListOption) ([]domain.FieldChange, error) {
	return nil, nil
}
func (m *MockQuery) FindByActor(_ context.Context, _ string, _ ...port.ListOption) ([]domain.VersionRecord, error) {
	return nil, nil
}
func (m *MockQuery) GetBulkAtTime(_ context.Context, _ string, _ []string, _ time.Time) (map[string]domain.VersionRecord, error) {
	return nil, nil
}

// MockSchema is a minimal mock for SchemaPort.
type MockSchema struct{}

func (m *MockSchema) RegisterMigration(_ string, _, _ int, _ port.MigrationFunc) error { return nil }
func (m *MockSchema) VerifyIntegrity(_ context.Context, _, _ string) error             { return nil }
