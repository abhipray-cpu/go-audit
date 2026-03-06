package audittest

import (
	"testing"
)

func TestSeedUserHistory(t *testing.T) {
	a, store := NewAuditor(t)

	results := SeedUserHistory(t, a, "u1", 5)

	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}
	if store.Len() != 5 {
		t.Fatalf("expected 5 records in store, got %d", store.Len())
	}

	// Verify versions are monotonic.
	for i, r := range results {
		want := int64(i + 1)
		if r.Record.Version != want {
			t.Errorf("result[%d].Record.Version = %d, want %d", i, r.Record.Version, want)
		}
	}

	// Verify entity type.
	if results[0].Record.EntityType != "fixtureuser" {
		t.Errorf("EntityType = %q, want %q", results[0].Record.EntityType, "fixtureuser")
	}
}

func TestSeedUserHistory_Zero(t *testing.T) {
	a, store := NewAuditor(t)

	results := SeedUserHistory(t, a, "u1", 0)

	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
	if store.Len() != 0 {
		t.Errorf("expected 0 records, got %d", store.Len())
	}
}

func TestFixtureUser_AllTags(t *testing.T) {
	a, _ := NewAuditor(t)

	// FixtureUser should register without error (has all 5 tag types).
	if err := a.Register(&FixtureUser{}); err != nil {
		t.Fatalf("Register(FixtureUser) error: %v", err)
	}

	cfg, err := a.LookupEntity(&FixtureUser{ID: "test"})
	if err != nil {
		t.Fatalf("LookupEntity() error: %v", err)
	}
	if cfg.TypeName != "fixtureuser" {
		t.Errorf("TypeName = %q, want %q", cfg.TypeName, "fixtureuser")
	}
	if cfg.IDField != "ID" {
		t.Errorf("IDField = %q, want %q", cfg.IDField, "ID")
	}
}

func TestFixtureAddress_Register(t *testing.T) {
	a, _ := NewAuditor(t)

	if err := a.Register(&FixtureAddress{}); err != nil {
		t.Fatalf("Register(FixtureAddress) error: %v", err)
	}

	cfg, err := a.LookupEntity(&FixtureAddress{ID: "addr1"})
	if err != nil {
		t.Fatalf("LookupEntity() error: %v", err)
	}
	if cfg.TypeName != "fixtureaddress" {
		t.Errorf("TypeName = %q, want %q", cfg.TypeName, "fixtureaddress")
	}
}
