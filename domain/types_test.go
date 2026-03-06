package domain

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestVersionRecord_Fields(t *testing.T) {
	now := time.Now().UTC()

	rec := VersionRecord{
		ID:            "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		EntityType:    "user",
		EntityID:      "user-123",
		Version:       3,
		Strategy:      StrategyDelta,
		SchemaVersion: 2,
		Data:          []byte(`{"name":"Alice"}`),
		ContentType:   "application/json",
		Delta: &Delta{
			Changes: []FieldChange{
				{Path: "Name", OldValue: json.RawMessage(`"Bob"`), NewValue: json.RawMessage(`"Alice"`)},
			},
		},
		Metadata: VersionMetadata{
			ActorID:       "admin-1",
			ActorType:     ActorHuman,
			Reason:        "profile update",
			CorrelationID: "req-456",
			TraceID:       "abc123",
			SpanID:        "span-789",
			Source:        "api",
			Custom:        map[string]string{"tenant": "acme"},
			Timestamp:     now,
		},
		Hash:         "sha256:aabbccdd",
		PreviousHash: "sha256:11223344",
		CreatedAt:    now,
	}

	// Verify all fields are set correctly.
	if rec.ID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Errorf("ID = %q, want %q", rec.ID, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	}
	if rec.EntityType != "user" {
		t.Errorf("EntityType = %q, want %q", rec.EntityType, "user")
	}
	if rec.EntityID != "user-123" {
		t.Errorf("EntityID = %q, want %q", rec.EntityID, "user-123")
	}
	if rec.Version != 3 {
		t.Errorf("Version = %d, want %d", rec.Version, 3)
	}
	if rec.Strategy != StrategyDelta {
		t.Errorf("Strategy = %v, want %v", rec.Strategy, StrategyDelta)
	}
	if rec.SchemaVersion != 2 {
		t.Errorf("SchemaVersion = %d, want %d", rec.SchemaVersion, 2)
	}
	if string(rec.Data) != `{"name":"Alice"}` {
		t.Errorf("Data = %s, want %s", rec.Data, `{"name":"Alice"}`)
	}
	if rec.ContentType != "application/json" {
		t.Errorf("ContentType = %q, want %q", rec.ContentType, "application/json")
	}
	if rec.Delta == nil || len(rec.Delta.Changes) != 1 {
		t.Fatalf("Delta.Changes length = %d, want 1", len(rec.Delta.Changes))
	}
	if rec.Delta.Changes[0].Path != "Name" {
		t.Errorf("Delta.Changes[0].Path = %q, want %q", rec.Delta.Changes[0].Path, "Name")
	}
	if rec.Metadata.ActorID != "admin-1" {
		t.Errorf("Metadata.ActorID = %q, want %q", rec.Metadata.ActorID, "admin-1")
	}
	if rec.Metadata.ActorType != ActorHuman {
		t.Errorf("Metadata.ActorType = %v, want %v", rec.Metadata.ActorType, ActorHuman)
	}
	if rec.Metadata.Reason != "profile update" {
		t.Errorf("Metadata.Reason = %q, want %q", rec.Metadata.Reason, "profile update")
	}
	if rec.Metadata.CorrelationID != "req-456" {
		t.Errorf("Metadata.CorrelationID = %q, want %q", rec.Metadata.CorrelationID, "req-456")
	}
	if rec.Metadata.Custom["tenant"] != "acme" {
		t.Errorf("Metadata.Custom[tenant] = %q, want %q", rec.Metadata.Custom["tenant"], "acme")
	}
	if rec.Hash != "sha256:aabbccdd" {
		t.Errorf("Hash = %q, want %q", rec.Hash, "sha256:aabbccdd")
	}
	if rec.PreviousHash != "sha256:11223344" {
		t.Errorf("PreviousHash = %q, want %q", rec.PreviousHash, "sha256:11223344")
	}
	if !rec.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", rec.CreatedAt, now)
	}
}

func TestStrategy_Constants(t *testing.T) {
	tests := []struct {
		strategy Strategy
		value    int
		name     string
	}{
		{StrategyFull, 0, "full"},
		{StrategyDelta, 1, "delta"},
		{StrategyHybrid, 2, "hybrid"},
	}

	for _, tt := range tests {
		if int(tt.strategy) != tt.value {
			t.Errorf("Strategy %s = %d, want %d", tt.name, int(tt.strategy), tt.value)
		}
		if tt.strategy.String() != tt.name {
			t.Errorf("Strategy(%d).String() = %q, want %q", tt.value, tt.strategy.String(), tt.name)
		}
	}
}

func TestStrategy_Unknown(t *testing.T) {
	s := Strategy(99)
	if s.String() != "unknown" {
		t.Errorf("Strategy(99).String() = %q, want %q", s.String(), "unknown")
	}
}

func TestActorType_Constants(t *testing.T) {
	tests := []struct {
		actorType ActorType
		value     int
		name      string
	}{
		{ActorHuman, 0, "human"},
		{ActorSystem, 1, "system"},
		{ActorService, 2, "service"},
	}

	for _, tt := range tests {
		if int(tt.actorType) != tt.value {
			t.Errorf("ActorType %s = %d, want %d", tt.name, int(tt.actorType), tt.value)
		}
		if tt.actorType.String() != tt.name {
			t.Errorf("ActorType(%d).String() = %q, want %q", tt.value, tt.actorType.String(), tt.name)
		}
	}
}

func TestActorType_Unknown(t *testing.T) {
	a := ActorType(99)
	if a.String() != "unknown" {
		t.Errorf("ActorType(99).String() = %q, want %q", a.String(), "unknown")
	}
}

func TestVersionRecord_JSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC)

	original := VersionRecord{
		ID:            "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		EntityType:    "order",
		EntityID:      "order-789",
		Version:       1,
		Strategy:      StrategyFull,
		SchemaVersion: 1,
		Data:          []byte(`{"total":99.99}`),
		ContentType:   "application/json",
		Delta:         nil,
		Metadata: VersionMetadata{
			ActorID:       "svc-checkout",
			ActorType:     ActorService,
			Reason:        "order placed",
			CorrelationID: "tx-abc",
			Source:        "checkout-api",
			Custom:        map[string]string{"region": "us-east-1"},
			Timestamp:     now,
		},
		Hash:         "sha256:deadbeef",
		PreviousHash: "",
		CreatedAt:    now,
	}

	// Marshal to JSON.
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// Unmarshal back.
	var decoded VersionRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// Verify round-trip fidelity.
	if decoded.ID != original.ID {
		t.Errorf("ID mismatch: %q != %q", decoded.ID, original.ID)
	}
	if decoded.EntityType != original.EntityType {
		t.Errorf("EntityType mismatch: %q != %q", decoded.EntityType, original.EntityType)
	}
	if decoded.EntityID != original.EntityID {
		t.Errorf("EntityID mismatch: %q != %q", decoded.EntityID, original.EntityID)
	}
	if decoded.Version != original.Version {
		t.Errorf("Version mismatch: %d != %d", decoded.Version, original.Version)
	}
	if decoded.Strategy != original.Strategy {
		t.Errorf("Strategy mismatch: %v != %v", decoded.Strategy, original.Strategy)
	}
	if decoded.SchemaVersion != original.SchemaVersion {
		t.Errorf("SchemaVersion mismatch: %d != %d", decoded.SchemaVersion, original.SchemaVersion)
	}
	if string(decoded.Data) != string(original.Data) {
		t.Errorf("Data mismatch: %s != %s", decoded.Data, original.Data)
	}
	if decoded.ContentType != original.ContentType {
		t.Errorf("ContentType mismatch: %q != %q", decoded.ContentType, original.ContentType)
	}
	if decoded.Delta != nil {
		t.Errorf("Delta should be nil, got %+v", decoded.Delta)
	}
	if decoded.Metadata.ActorID != original.Metadata.ActorID {
		t.Errorf("Metadata.ActorID mismatch: %q != %q", decoded.Metadata.ActorID, original.Metadata.ActorID)
	}
	if decoded.Metadata.ActorType != original.Metadata.ActorType {
		t.Errorf("Metadata.ActorType mismatch: %v != %v", decoded.Metadata.ActorType, original.Metadata.ActorType)
	}
	if decoded.Metadata.Reason != original.Metadata.Reason {
		t.Errorf("Metadata.Reason mismatch: %q != %q", decoded.Metadata.Reason, original.Metadata.Reason)
	}
	if decoded.Metadata.CorrelationID != original.Metadata.CorrelationID {
		t.Errorf("Metadata.CorrelationID mismatch: %q != %q", decoded.Metadata.CorrelationID, original.Metadata.CorrelationID)
	}
	if decoded.Metadata.Custom["region"] != "us-east-1" {
		t.Errorf("Metadata.Custom[region] mismatch: %q", decoded.Metadata.Custom["region"])
	}
	if decoded.Hash != original.Hash {
		t.Errorf("Hash mismatch: %q != %q", decoded.Hash, original.Hash)
	}
	if decoded.PreviousHash != original.PreviousHash {
		t.Errorf("PreviousHash mismatch: %q != %q", decoded.PreviousHash, original.PreviousHash)
	}
	if !decoded.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt mismatch: %v != %v", decoded.CreatedAt, original.CreatedAt)
	}
}

func TestVersionMetadata_JSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	original := VersionMetadata{
		ActorID:       "user-42",
		ActorType:     ActorHuman,
		Reason:        "manual edit",
		CorrelationID: "corr-1",
		TraceID:       "trace-abc",
		SpanID:        "span-def",
		Source:        "web-ui",
		Custom:        map[string]string{"ip": "10.0.0.1"},
		Timestamp:     now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded VersionMetadata
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.ActorID != original.ActorID {
		t.Errorf("ActorID mismatch")
	}
	if decoded.ActorType != original.ActorType {
		t.Errorf("ActorType mismatch: %v != %v", decoded.ActorType, original.ActorType)
	}
	if decoded.TraceID != original.TraceID {
		t.Errorf("TraceID mismatch")
	}
	if decoded.SpanID != original.SpanID {
		t.Errorf("SpanID mismatch")
	}
	if decoded.Source != original.Source {
		t.Errorf("Source mismatch")
	}
	if !decoded.Timestamp.Equal(original.Timestamp) {
		t.Errorf("Timestamp mismatch")
	}
}

func TestDelta_IsEmpty(t *testing.T) {
	empty := Delta{}
	if !empty.IsEmpty() {
		t.Error("empty delta should report IsEmpty() = true")
	}

	nonEmpty := Delta{
		Changes: []FieldChange{
			{Path: "Name", OldValue: json.RawMessage(`"A"`), NewValue: json.RawMessage(`"B"`)},
		},
	}
	if nonEmpty.IsEmpty() {
		t.Error("non-empty delta should report IsEmpty() = false")
	}
}

func TestFieldChange_JSONRoundTrip(t *testing.T) {
	fc := FieldChange{
		Path:     "Address.City",
		OldValue: json.RawMessage(`"New York"`),
		NewValue: json.RawMessage(`"San Francisco"`),
	}

	data, err := json.Marshal(fc)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded FieldChange
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.Path != fc.Path {
		t.Errorf("Path mismatch: %q != %q", decoded.Path, fc.Path)
	}
	if string(decoded.OldValue) != string(fc.OldValue) {
		t.Errorf("OldValue mismatch: %s != %s", decoded.OldValue, fc.OldValue)
	}
	if string(decoded.NewValue) != string(fc.NewValue) {
		t.Errorf("NewValue mismatch: %s != %s", decoded.NewValue, fc.NewValue)
	}
}

func TestPendingVersion_Wait_Success(t *testing.T) {
	pv := NewPendingVersion()

	expected := VersionRecord{
		ID:         "test-id",
		EntityType: "user",
		EntityID:   "user-1",
		Version:    1,
	}

	// Complete in a goroutine (simulating background worker).
	go func() {
		pv.Complete(expected, nil)
	}()

	result, err := pv.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if result.ID != expected.ID {
		t.Errorf("Record.ID = %q, want %q", result.ID, expected.ID)
	}
	if result.Version != expected.Version {
		t.Errorf("Record.Version = %d, want %d", result.Version, expected.Version)
	}
}

func TestPendingVersion_Wait_ContextCancelled(t *testing.T) {
	pv := NewPendingVersion()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := pv.Wait(ctx)
	if err != context.Canceled {
		t.Errorf("Wait error = %v, want context.Canceled", err)
	}
}

func TestPendingVersion_Done_Channel(t *testing.T) {
	pv := NewPendingVersion()

	// Channel should not be closed yet.
	select {
	case <-pv.Done():
		t.Fatal("Done() channel should not be closed before Complete()")
	default:
		// Expected.
	}

	pv.Complete(VersionRecord{Version: 5}, nil)

	// Channel should now be closed.
	select {
	case <-pv.Done():
		// Expected.
	default:
		t.Fatal("Done() channel should be closed after Complete()")
	}

	if pv.Err() != nil {
		t.Errorf("Err() = %v, want nil", pv.Err())
	}
	if pv.Record().Version != 5 {
		t.Errorf("Record().Version = %d, want 5", pv.Record().Version)
	}
}

func TestPendingVersion_Complete_WithError(t *testing.T) {
	pv := NewPendingVersion()

	expectedErr := context.DeadlineExceeded
	pv.Complete(VersionRecord{}, expectedErr)

	<-pv.Done()

	if pv.Err() != expectedErr {
		t.Errorf("Err() = %v, want %v", pv.Err(), expectedErr)
	}
}

func TestVersionResult_Fields(t *testing.T) {
	vr := VersionResult{
		Record: VersionRecord{
			ID:      "test-id",
			Version: 3,
		},
		Duration: 150 * time.Millisecond,
		Strategy: StrategyHybrid,
	}

	if vr.Record.ID != "test-id" {
		t.Errorf("Record.ID = %q, want %q", vr.Record.ID, "test-id")
	}
	if vr.Duration != 150*time.Millisecond {
		t.Errorf("Duration = %v, want 150ms", vr.Duration)
	}
	if vr.Strategy != StrategyHybrid {
		t.Errorf("Strategy = %v, want StrategyHybrid", vr.Strategy)
	}
}
