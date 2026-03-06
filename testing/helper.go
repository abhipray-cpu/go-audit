package audittest

import (
	"testing"

	"github.com/abhipray-cpu/go-audit"
)

// NewAuditor creates a fully-wired [audit.Auditor] backed by an
// [InMemoryStore]. All adapter defaults (JSON serializer, reflect cloner,
// slog logger, diff engine, context metadata extractor) are applied.
//
// The returned store can be used to inspect persisted records.
//
// This helper calls t.Fatal on configuration errors (which should never
// happen with the default config).
func NewAuditor(t *testing.T) (*audit.Auditor, *InMemoryStore) {
	t.Helper()

	store := NewInMemoryStore()

	a, err := audit.New(audit.Config{
		Writer: store,
		Reader: store,
	})
	if err != nil {
		t.Fatalf("audittest.NewAuditor: %v", err)
	}

	return a, store
}
