package audittest

import (
	"context"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// AssertVersionCount checks that the store contains exactly n version
// records for the given entity.
func AssertVersionCount(t *testing.T, reader port.VersionReaderPort, entityType, entityID string, n int) {
	t.Helper()

	recs, err := reader.ListVersions(context.Background(), entityType, entityID, port.WithLimit(10000))
	if err != nil {
		t.Fatalf("AssertVersionCount: ListVersions error: %v", err)
	}
	if len(recs) != n {
		t.Errorf("AssertVersionCount(%s/%s): got %d versions, want %d", entityType, entityID, len(recs), n)
	}
}

// AssertFieldChanged checks that a specific field was changed in the given
// version's delta. It verifies the field appears in Delta.Changes by path.
func AssertFieldChanged(t *testing.T, record domain.VersionRecord, fieldPath string) {
	t.Helper()

	if record.Delta == nil {
		t.Fatalf("AssertFieldChanged(%q): record v%d has no delta", fieldPath, record.Version)
	}

	for _, ch := range record.Delta.Changes {
		if ch.Path == fieldPath {
			return
		}
	}
	t.Errorf("AssertFieldChanged(%q): field not found in delta changes (v%d)", fieldPath, record.Version)
}

// AssertActorIs checks that the version record's metadata has the expected
// actor ID.
func AssertActorIs(t *testing.T, record domain.VersionRecord, expectedActorID string) {
	t.Helper()

	if record.Metadata.ActorID != expectedActorID {
		t.Errorf("AssertActorIs: got ActorID %q, want %q (v%d)",
			record.Metadata.ActorID, expectedActorID, record.Version)
	}
}

// AssertLatestVersion checks that the latest version number for the entity
// matches the expected value.
func AssertLatestVersion(t *testing.T, reader port.VersionReaderPort, entityType, entityID string, expectedVersion int64) {
	t.Helper()

	rec, err := reader.GetLatest(context.Background(), entityType, entityID)
	if err != nil {
		t.Fatalf("AssertLatestVersion: GetLatest error: %v", err)
	}
	if rec.Version != expectedVersion {
		t.Errorf("AssertLatestVersion(%s/%s): got v%d, want v%d",
			entityType, entityID, rec.Version, expectedVersion)
	}
}
