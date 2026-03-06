package audittest

import (
	"context"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------- Shared test entities ----------

// FixtureUser is a canonical test entity that exercises all five field tag
// types used by go-audit's diff engine and registry.
type FixtureUser struct {
	ID       string            `version:"id"`         // entity identifier
	Name     string            `version:"tracked"`    // track changes
	Email    string            `version:"tracked"`    // track changes
	Password string            `version:"ignore"`     // never versioned
	Tags     []string          `version:"normalized"` // sorted before diff
	Metadata map[string]string `version:"unordered"`  // ignore key order
}

// FixtureAddress is an auxiliary entity for testing nested and multi-entity
// scenarios.
type FixtureAddress struct {
	ID      string `version:"id"`
	UserID  string `version:"tracked"`
	Street  string `version:"tracked"`
	City    string `version:"tracked"`
	Country string `version:"tracked"`
	Zip     string `version:"tracked"`
}

// ---------- Seed helpers ----------

// SeedUserHistory registers [FixtureUser] (if not already registered),
// then creates `versions` sequential version records for the given entityID.
// Each version mutates the Name field so that diffs are non-empty.
//
// Returns the slice of [domain.VersionResult] for all created versions.
func SeedUserHistory(t interface {
	Helper()
	Fatalf(format string, args ...any)
}, auditor *audit.Auditor, entityID string, versions int) []domain.VersionResult {
	t.Helper()

	// Attempt to register — ignore duplicate error (already registered).
	_ = auditor.Register(&FixtureUser{})

	results := make([]domain.VersionResult, 0, versions)
	ctx := context.Background()

	for i := 1; i <= versions; i++ {
		user := &FixtureUser{
			ID:    entityID,
			Name:  nameForVersion(i),
			Email: entityID + "@example.com",
			Tags:  []string{"seed"},
			Metadata: map[string]string{
				"version": string(rune('0' + i)),
			},
		}

		pending, err := auditor.Version(ctx, user, port.WithSync())
		if err != nil {
			t.Fatalf("SeedUserHistory: Version() v%d error: %v", i, err)
		}
		record, waitErr := pending.Wait(ctx)
		if waitErr != nil {
			t.Fatalf("SeedUserHistory: Wait() v%d error: %v", i, waitErr)
		}
		results = append(results, domain.VersionResult{Record: record})
	}

	return results
}

// nameForVersion generates a deterministic name for the given version number.
func nameForVersion(v int) string {
	names := []string{
		"Alice", "Bob", "Charlie", "Diana", "Eve",
		"Frank", "Grace", "Heidi", "Ivan", "Judy",
	}
	return names[v%len(names)]
}
