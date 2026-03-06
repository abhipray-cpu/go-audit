package domain

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrors_Is_Unwrap(t *testing.T) {
	// Test that sentinel errors are distinct.
	sentinels := []error{
		ErrStorage,
		ErrDiff,
		ErrSerialization,
		ErrValidation,
		ErrConfiguration,
		ErrWAL,
		ErrMigration,
		ErrDiffFallback,
		ErrDuplicateVersion,
		ErrNotFound,
		ErrTimeout,
		ErrEntityTooLarge,
		ErrMetadataTooLarge,
	}

	for i, a := range sentinels {
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Errorf("errors.Is(%q, %q) = true, want false", a, b)
			}
		}
	}

	// Test that wrapped errors preserve Is() semantics.
	wrapped := Wrap(ErrStorage, "pg connection failed: %s", "timeout")
	if !errors.Is(wrapped, ErrStorage) {
		t.Error("errors.Is(wrapped, ErrStorage) = false, want true")
	}
	if errors.Is(wrapped, ErrDiff) {
		t.Error("errors.Is(wrapped, ErrDiff) = true, want false")
	}

	// Test double-wrap preserves both sentinels.
	doubleWrapped := fmt.Errorf("operation failed: %w", Wrap(ErrStorage, "pg: %s", "down"))
	if !errors.Is(doubleWrapped, ErrStorage) {
		t.Error("errors.Is(doubleWrapped, ErrStorage) = false, want true")
	}

	// Test errors.As with EntityTooLargeError.
	entityErr := &EntityTooLargeError{EntityType: "user", Size: 10_000_000, Limit: 5_000_000}
	wrappedEntity := fmt.Errorf("version failed: %w", entityErr)

	var target *EntityTooLargeError
	if !errors.As(wrappedEntity, &target) {
		t.Fatal("errors.As(wrappedEntity, *EntityTooLargeError) = false, want true")
	}
	if target.EntityType != "user" {
		t.Errorf("EntityType = %q, want %q", target.EntityType, "user")
	}
	if target.Size != 10_000_000 {
		t.Errorf("Size = %d, want %d", target.Size, 10_000_000)
	}
	if target.Limit != 5_000_000 {
		t.Errorf("Limit = %d, want %d", target.Limit, 5_000_000)
	}

	// EntityTooLargeError unwraps to both ErrEntityTooLarge and ErrValidation.
	if !errors.Is(entityErr, ErrEntityTooLarge) {
		t.Error("errors.Is(EntityTooLargeError, ErrEntityTooLarge) = false, want true")
	}
	if !errors.Is(entityErr, ErrValidation) {
		t.Error("errors.Is(EntityTooLargeError, ErrValidation) = false, want true")
	}

	// MetadataTooLargeError unwraps to both ErrMetadataTooLarge and ErrValidation.
	metaErr := &MetadataTooLargeError{Size: 2_000_000, Limit: 1_000_000}
	if !errors.Is(metaErr, ErrMetadataTooLarge) {
		t.Error("errors.Is(MetadataTooLargeError, ErrMetadataTooLarge) = false, want true")
	}
	if !errors.Is(metaErr, ErrValidation) {
		t.Error("errors.Is(MetadataTooLargeError, ErrValidation) = false, want true")
	}

	var metaTarget *MetadataTooLargeError
	wrappedMeta := fmt.Errorf("meta issue: %w", metaErr)
	if !errors.As(wrappedMeta, &metaTarget) {
		t.Fatal("errors.As(wrappedMeta, *MetadataTooLargeError) = false, want true")
	}
	if metaTarget.Size != 2_000_000 {
		t.Errorf("MetadataTooLargeError.Size = %d, want %d", metaTarget.Size, 2_000_000)
	}
}

func TestErrEntityTooLarge_Message(t *testing.T) {
	err := &EntityTooLargeError{
		EntityType: "order",
		Size:       10_485_760,
		Limit:      5_242_880,
	}

	msg := err.Error()

	// Must include entity type.
	if !containsSubstring(msg, "order") {
		t.Errorf("error message %q does not contain entity type %q", msg, "order")
	}

	// Must include actual size.
	if !containsSubstring(msg, "10485760") {
		t.Errorf("error message %q does not contain size %q", msg, "10485760")
	}

	// Must include limit.
	if !containsSubstring(msg, "5242880") {
		t.Errorf("error message %q does not contain limit %q", msg, "5242880")
	}
}

func TestErrMetadataTooLarge_Message(t *testing.T) {
	err := &MetadataTooLargeError{
		Size:  2_097_152,
		Limit: 1_048_576,
	}

	msg := err.Error()

	// Must include actual size.
	if !containsSubstring(msg, "2097152") {
		t.Errorf("error message %q does not contain size %q", msg, "2097152")
	}

	// Must include limit.
	if !containsSubstring(msg, "1048576") {
		t.Errorf("error message %q does not contain limit %q", msg, "1048576")
	}
}

func TestWrap_PreservesSentinel(t *testing.T) {
	cause := fmt.Errorf("connection refused")
	wrapped := Wrap(ErrStorage, "pg write: %w", cause)

	if !errors.Is(wrapped, ErrStorage) {
		t.Error("Wrap should preserve sentinel via errors.Is")
	}

	// The message should contain the context.
	msg := wrapped.Error()
	if !containsSubstring(msg, "pg write") {
		t.Errorf("wrapped message %q does not contain context %q", msg, "pg write")
	}
	if !containsSubstring(msg, "connection refused") {
		t.Errorf("wrapped message %q does not contain cause %q", msg, "connection refused")
	}
}

func TestSentinelErrors_HaveDescriptiveMessages(t *testing.T) {
	tests := []struct {
		err      error
		contains string
	}{
		{ErrStorage, "storage"},
		{ErrDiff, "diff"},
		{ErrSerialization, "serialization"},
		{ErrValidation, "validation"},
		{ErrConfiguration, "configuration"},
		{ErrWAL, "WAL"},
		{ErrMigration, "migration"},
		{ErrDiffFallback, "fallback"},
		{ErrDuplicateVersion, "duplicate"},
		{ErrNotFound, "not found"},
		{ErrTimeout, "timeout"},
		{ErrEntityTooLarge, "entity too large"},
		{ErrMetadataTooLarge, "metadata too large"},
	}

	for _, tt := range tests {
		if !containsSubstring(tt.err.Error(), tt.contains) {
			t.Errorf("error %q does not contain %q", tt.err.Error(), tt.contains)
		}
	}
}

// containsSubstring reports whether s contains substr (case-sensitive).
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
