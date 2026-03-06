package domain

import (
	"errors"
	"fmt"
)

// Sentinel error categories. Use [errors.Is] to check error categories
// and [errors.As] to extract contextual error details.
var (
	// ErrStorage indicates a failure in the underlying storage backend
	// (database unreachable, query failed, connection pool exhausted).
	ErrStorage = errors.New("audit: storage error")

	// ErrDiff indicates a failure in the diff engine while comparing
	// two versions of an entity.
	ErrDiff = errors.New("audit: diff error")

	// ErrSerialization indicates a failure marshaling or unmarshaling
	// entity data (corrupted payload, incompatible format).
	ErrSerialization = errors.New("audit: serialization error")

	// ErrValidation indicates that an input failed validation
	// (missing required field, invalid entity ID, nil transaction).
	ErrValidation = errors.New("audit: validation error")

	// ErrConfiguration indicates an invalid or missing configuration
	// (no database, unregistered entity, missing ID field).
	ErrConfiguration = errors.New("audit: configuration error")

	// ErrWAL indicates a failure in the Write-Ahead Log
	// (append failed, replay corrupted, CRC mismatch).
	ErrWAL = errors.New("audit: WAL error")

	// ErrMigration indicates a failure during schema evolution
	// (missing migration step, incompatible schema versions).
	ErrMigration = errors.New("audit: migration error")

	// ErrDiffFallback indicates that the diff engine encountered an
	// unsupported type and fell back to full-snapshot mode (ADR-005).
	// This is a warning-level error — the version was still created.
	ErrDiffFallback = errors.New("audit: diff fallback to full snapshot")

	// ErrDuplicateVersion indicates that a version with the same
	// (entity_type, entity_id, version) tuple already exists.
	ErrDuplicateVersion = errors.New("audit: duplicate version")

	// ErrNotFound indicates that the requested version record does not exist.
	ErrNotFound = errors.New("audit: not found")

	// ErrTimeout indicates that an operation exceeded its deadline.
	ErrTimeout = errors.New("audit: timeout")

	// ErrEntityTooLarge indicates that a serialized entity snapshot exceeds
	// the configured [Config.MaxEntitySizeBytes] limit.
	ErrEntityTooLarge = errors.New("audit: entity too large")

	// ErrMetadataTooLarge indicates that version metadata exceeds the
	// configured [Config.MaxMetadataSizeBytes] limit.
	ErrMetadataTooLarge = errors.New("audit: metadata too large")
)

// EntityTooLargeError is returned when an entity exceeds the maximum
// allowed size. It includes the actual size and the configured limit.
type EntityTooLargeError struct {
	// EntityType is the registered type name of the entity.
	EntityType string

	// Size is the actual size of the entity in bytes.
	Size int64

	// Limit is the maximum allowed size in bytes.
	Limit int64
}

// Error returns a human-readable message including the entity type, size, and limit.
func (e *EntityTooLargeError) Error() string {
	return fmt.Sprintf(
		"audit: entity %q too large: %d bytes exceeds limit of %d bytes",
		e.EntityType, e.Size, e.Limit,
	)
}

// Unwrap returns both [ErrEntityTooLarge] and [ErrValidation] so that
// errors.Is works with either sentinel.
func (e *EntityTooLargeError) Unwrap() []error {
	return []error{ErrEntityTooLarge, ErrValidation}
}

// MetadataTooLargeError is returned when version metadata exceeds the maximum
// allowed size. It includes the actual size and the configured limit.
type MetadataTooLargeError struct {
	// Size is the actual size of the metadata in bytes.
	Size int64

	// Limit is the maximum allowed size in bytes.
	Limit int64
}

// Error returns a human-readable message including the size and limit.
func (e *MetadataTooLargeError) Error() string {
	return fmt.Sprintf(
		"audit: metadata too large: %d bytes exceeds limit of %d bytes",
		e.Size, e.Limit,
	)
}

// Unwrap returns both [ErrMetadataTooLarge] and [ErrValidation] so that
// errors.Is works with either sentinel.
func (e *MetadataTooLargeError) Unwrap() []error {
	return []error{ErrMetadataTooLarge, ErrValidation}
}

// Wrap wraps a sentinel error with additional context while preserving
// the error chain for [errors.Is] and [errors.As].
//
//	return domain.Wrap(domain.ErrStorage, "postgres write failed: %w", pgErr)
func Wrap(sentinel error, format string, args ...any) error {
	inner := fmt.Errorf(format, args...)
	return fmt.Errorf("%w: %w", sentinel, inner)
}
