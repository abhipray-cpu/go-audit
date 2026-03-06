package domain

import (
	"context"
	"encoding/json"
	"time"
)

// Strategy defines how a version record stores entity state.
type Strategy int

const (
	// StrategyFull stores the complete serialized entity on every version.
	StrategyFull Strategy = iota

	// StrategyDelta stores only the field-level changes (delta) relative
	// to the previous version. The first version is always a full snapshot.
	StrategyDelta

	// StrategyHybrid stores a full snapshot every N versions and deltas
	// between snapshots. This bounds reconstruction cost.
	StrategyHybrid
)

// String returns the human-readable name of the strategy.
func (s Strategy) String() string {
	switch s {
	case StrategyFull:
		return "full"
	case StrategyDelta:
		return "delta"
	case StrategyHybrid:
		return "hybrid"
	default:
		return "unknown"
	}
}

// MarshalJSON encodes the strategy as a JSON string.
func (s Strategy) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON decodes a strategy from a JSON string.
func (s *Strategy) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	switch str {
	case "full":
		*s = StrategyFull
	case "delta":
		*s = StrategyDelta
	case "hybrid":
		*s = StrategyHybrid
	default:
		return &json.UnmarshalTypeError{
			Value: "string " + str,
			Type:  nil,
		}
	}
	return nil
}

// ActorType identifies the kind of actor that triggered a version.
type ActorType int

const (
	// ActorHuman represents a human user.
	ActorHuman ActorType = iota

	// ActorSystem represents an automated system process.
	ActorSystem

	// ActorService represents an external service or API client.
	ActorService
)

// String returns the human-readable name of the actor type.
func (a ActorType) String() string {
	switch a {
	case ActorHuman:
		return "human"
	case ActorSystem:
		return "system"
	case ActorService:
		return "service"
	default:
		return "unknown"
	}
}

// MarshalJSON encodes the actor type as a JSON string.
func (a ActorType) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.String())
}

// UnmarshalJSON decodes an actor type from a JSON string.
func (a *ActorType) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	switch str {
	case "human":
		*a = ActorHuman
	case "system":
		*a = ActorSystem
	case "service":
		*a = ActorService
	default:
		return &json.UnmarshalTypeError{
			Value: "string " + str,
			Type:  nil,
		}
	}
	return nil
}

// VersionMetadata holds contextual information about who created a version
// and why. It is stored alongside every [VersionRecord].
type VersionMetadata struct {
	// ActorID identifies the user, service, or system that created the version.
	ActorID string `json:"actor_id"`

	// ActorType classifies the actor (human, system, or service).
	ActorType ActorType `json:"actor_type"`

	// Reason is a human-readable description of why the change was made.
	Reason string `json:"reason"`

	// CorrelationID links related changes across services.
	CorrelationID string `json:"correlation_id"`

	// TraceID is the OpenTelemetry trace ID, if available.
	TraceID string `json:"trace_id"`

	// SpanID is the OpenTelemetry span ID, if available.
	SpanID string `json:"span_id"`

	// Source identifies the origin of the change (e.g., "api", "migration").
	Source string `json:"source"`

	// Custom holds arbitrary key-value metadata supplied by the caller.
	Custom map[string]string `json:"custom"`

	// Timestamp records when the metadata was captured.
	Timestamp time.Time `json:"timestamp"`
}

// FieldChange describes a single field-level change between two versions
// of an entity. Path uses dotted notation (e.g., "Address.City").
type FieldChange struct {
	// Path is the dotted field path (e.g., "Address.City", "Settings.notifications.sms").
	Path string `json:"path"`

	// OldValue is the JSON-encoded previous value, or nil if the field was added.
	OldValue json.RawMessage `json:"old_value"`

	// NewValue is the JSON-encoded current value, or nil if the field was removed.
	NewValue json.RawMessage `json:"new_value"`
}

// Delta represents the set of field-level changes between two consecutive
// versions of an entity.
type Delta struct {
	// Changes is the ordered list of field changes.
	Changes []FieldChange `json:"changes"`
}

// IsEmpty returns true if the delta contains no changes.
func (d Delta) IsEmpty() bool {
	return len(d.Changes) == 0
}

// VersionRecord is the core domain entity: a single immutable version of an
// audited entity. Once written, a version record is never modified.
type VersionRecord struct {
	// ID is the unique identifier for this version record (ULID).
	ID string `json:"id"`

	// EntityType is the registered type name (e.g., "user").
	EntityType string `json:"entity_type"`

	// EntityID is the business identifier of the entity being versioned.
	EntityID string `json:"entity_id"`

	// Version is the monotonically increasing version number per entity.
	Version int64 `json:"version"`

	// Strategy indicates how the entity state is stored (full, delta, hybrid).
	Strategy Strategy `json:"strategy"`

	// SchemaVersion tracks structural changes to the entity over time.
	SchemaVersion int `json:"schema_version"`

	// Data holds the serialized entity (full snapshot) or delta, depending on Strategy.
	Data []byte `json:"data"`

	// ContentType indicates the serialization format (e.g., "application/json").
	ContentType string `json:"content_type"`

	// Delta holds the structured field-level changes (populated for delta/hybrid strategies).
	Delta *Delta `json:"delta,omitempty"`

	// Metadata holds contextual information about who created this version and why.
	Metadata VersionMetadata `json:"metadata"`

	// Hash is the SHA-256 hash of this record (for integrity verification).
	Hash string `json:"hash"`

	// PreviousHash is the hash of the preceding version record (for chain verification).
	PreviousHash string `json:"previous_hash"`

	// CreatedAt is the timestamp when this version was persisted.
	CreatedAt time.Time `json:"created_at"`
}

// VersionResult is the return value from a successful Version() call.
// It includes the persisted record plus operational metadata.
type VersionResult struct {
	// Record is the persisted version record.
	Record VersionRecord `json:"record"`

	// Duration is the wall-clock time spent creating this version.
	Duration time.Duration `json:"duration"`

	// Strategy is the strategy that was used for this version.
	Strategy Strategy `json:"strategy"`
}

// PendingVersion represents an in-flight version that has been enqueued
// for background processing. Callers can wait for completion or check
// the result asynchronously.
type PendingVersion struct {
	record VersionRecord
	err    error
	done   chan struct{}
}

// NewPendingVersion creates a new PendingVersion. The done channel should be
// closed and record/err set by the background worker upon completion.
func NewPendingVersion() *PendingVersion {
	return &PendingVersion{
		done: make(chan struct{}),
	}
}

// Complete marks the pending version as finished with the given record and error.
// It closes the done channel to unblock any waiters. Must be called exactly once.
func (p *PendingVersion) Complete(record VersionRecord, err error) {
	p.record = record
	p.err = err
	close(p.done)
}

// Wait blocks until the version is persisted or the context is cancelled.
// Returns the completed [VersionRecord] or an error.
func (p *PendingVersion) Wait(ctx context.Context) (VersionRecord, error) {
	select {
	case <-ctx.Done():
		return VersionRecord{}, ctx.Err()
	case <-p.done:
		return p.record, p.err
	}
}

// Done returns a channel that is closed when the version has been persisted
// (or has failed). Use this for non-blocking checks.
func (p *PendingVersion) Done() <-chan struct{} {
	return p.done
}

// Err returns the error from the background write, or nil if successful.
// Only valid after Done() is closed.
func (p *PendingVersion) Err() error {
	return p.err
}

// Record returns the completed VersionRecord.
// Only valid after Done() is closed.
func (p *PendingVersion) Record() VersionRecord {
	return p.record
}
